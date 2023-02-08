package actors

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	"github.com/pkg/errors"
	"time"
)

// CreateReminder ok
func (a *actorsRuntime) CreateReminder(ctx context.Context, req *CreateReminderRequest) error {
	if a.store == nil {
		return errors.New("actors: 状态存储不存在或配置不正确")
	}

	a.activeRemindersLock.Lock()
	defer a.activeRemindersLock.Unlock()
	// 如果存在符合条件的reminder
	if r, exists := a.getReminder(req); exists {
		if a.reminderRequiresUpdate(req, r) {
			err := a.DeleteReminder(ctx, &DeleteReminderRequest{
				ActorID:   req.ActorID,
				ActorType: req.ActorType,
				Name:      req.Name,
			})
			if err != nil {
				return err
			}
		} else {
			return nil
		}
	}

	// 在活动提醒列表中存储提醒信息
	actorKey := constructCompositeKey(req.ActorType, req.ActorID)
	reminderKey := constructCompositeKey(actorKey, req.Name)

	if a.evaluationBusy {
		select {
		case <-time.After(time.Second * 5):
			return errors.New("创建提醒时出错：5秒后超时了")
		case <-a.evaluationChan:
			break
		}
	}

	now := time.Now()
	reminder := Reminder{
		ActorID:   req.ActorID,
		ActorType: req.ActorType,
		Name:      req.Name,
		Data:      req.Data,
		Period:    req.Period,
		DueTime:   req.DueTime,
	}

	// 检查输入是否正确
	var (
		dueTime, ttl time.Time
		repeats      int
		err          error
	)
	if len(req.DueTime) != 0 {
		if dueTime, err = parseTime(req.DueTime, nil); err != nil {
			return errors.Wrap(err, "错误解析提醒到期时间")
		}
	} else {
		dueTime = now
	}
	reminder.RegisteredTime = dueTime.Format(time.RFC3339)

	if len(req.Period) != 0 {
		_, repeats, err = parseDuration(req.Period)
		if err != nil {
			return errors.Wrap(err, "错误解析提醒周期")
		}
		// 对重复次数为零的计时器有错误
		if repeats == 0 {
			return errors.Errorf("提醒%s的重复次数为零", reminder.Name)
		}
	}
	if len(req.TTL) > 0 {
		if ttl, err = parseTime(req.TTL, &dueTime); err != nil {
			return errors.Wrap(err, "error parsing reminder TTL")
		}
		if now.After(ttl) || dueTime.After(ttl) {
			return errors.Errorf("reminder %s 已经过期: registeredTime: %s TTL:%s",
				reminderKey, reminder.RegisteredTime, req.TTL)
		}
		reminder.ExpirationTime = ttl.UTC().Format(time.RFC3339)
	}

	stop := make(chan bool)
	a.activeReminders.Store(reminderKey, stop)

	err = backoff.Retry(func() error {
		// 将数据存储到actorStateStore中
		reminders, actorMetadata, err2 := a.getRemindersForActorType(req.ActorType, true)
		if err2 != nil {
			return err2
		}

		// 首先，我们把它添加到分区列表中。
		remindersInPartition, reminderRef, stateKey, etag := actorMetadata.insertReminderInPartition(reminders, &reminder)

		// 获取数据库分区密钥（CosmosDB需要）。
		databasePartitionKey := actorMetadata.calculateDatabasePartitionKey(stateKey)

		// 现在我们可以把它添加到 "全局 "列表中。
		reminders = append(reminders, reminderRef)

		// 然后，将该分区保存到数据库。
		err2 = a.saveRemindersInPartition(ctx, stateKey, remindersInPartition, etag, databasePartitionKey)
		if err2 != nil {
			return err2
		}

		// 最后，我们必须保存元数据以获得一个新的etag
		// 这就避免了更新和重新分区之间的竞赛条件。
		a.saveActorTypeMetadata(req.ActorType, actorMetadata)

		a.remindersLock.Lock()
		a.reminders[req.ActorType] = reminders
		a.remindersLock.Unlock()
		return nil
	}, backoff.NewExponentialBackOff())
	if err != nil {
		return err
	}
	return a.startReminder(&reminder, stop)
}

// CreateTimer ok
func (a *actorsRuntime) CreateTimer(ctx context.Context, req *CreateTimerRequest) error {
	var (
		err          error
		repeats      int
		dueTime, ttl time.Time
		period       time.Duration
	)
	a.activeTimersLock.Lock()
	defer a.activeTimersLock.Unlock()
	actorKey := constructCompositeKey(req.ActorType, req.ActorID)
	timerKey := constructCompositeKey(actorKey, req.Name)

	_, exists := a.actorsTable.Load(actorKey) // 判断actor存不存在
	if !exists {
		return errors.Errorf("不能创建actor: %s定时器: actor未激活", actorKey)
	}

	stopChan, exists := a.activeTimers.Load(timerKey) // 判断有没有创建过
	if exists {
		// 如果存在,关闭 stopChan
		close(stopChan.(chan bool))
	}

	if len(req.DueTime) != 0 {
		// 0h30m0s、R5/PT30M、P1MT2H10M3S、time.Now().Truncate(time.Minute).Add(time.Minute).Format(time.RFC3339)
		if dueTime, err = parseTime(req.DueTime, nil); err != nil {
			return errors.Wrap(err, "解析过期时间出错")
		}
		if time.Now().After(dueTime) {
			return errors.Errorf("定时器 %s 已经过期: 过期时间: %s 存活时间: %s", timerKey, req.DueTime, req.TTL)
		}
	} else {
		dueTime = time.Now()
	}

	repeats = -1 // set to default
	if len(req.Period) != 0 {
		// 解析时间、获的有多少秒数    0h30m0s  ---> 18好几个0 , -1 ,nil
		// R5/PT30M --->  18好几个0 , 5 ,nil
		if period, repeats, err = parseDuration(req.Period); err != nil {
			return errors.Wrap(err, "解析触发时长出错")
		}
		if repeats == 0 {
			return errors.Errorf("timer %s 0次触发", timerKey)
		}
	}

	if len(req.TTL) > 0 {
		//在过期时间上加上生存时间
		if ttl, err = parseTime(req.TTL, &dueTime); err != nil {
			return errors.Wrap(err, "解析定时器TTL出错")
		}
		//  为啥不判断  dueTime 相对于当前时间

		//  ------------------------------------------->
		//       👆🏻dueTime   👆🏻ttl            👆🏻now
		if time.Now().After(ttl) || dueTime.After(ttl) {
			return errors.Errorf("定时器 %s 已经过期: 过期时间: %s 存活时间: %s", timerKey, req.DueTime, req.TTL)
		}
	}

	log.Debugf("创建定时器 %q 到期时间:%s 周期:%s 重复:%d次 存活时间:%s",
		req.Name, dueTime.String(), period.String(), repeats, ttl.String())
	stop := make(chan bool, 1)
	a.activeTimers.Store(timerKey, stop)

	go func(stop chan bool, req *CreateTimerRequest) {
		var (
			ttlTimer, nextTimer *time.Timer
			ttlTimerC           <-chan time.Time
			err                 error
		)
		if !ttl.IsZero() {
			ttlTimer = time.NewTimer(time.Until(ttl))
			ttlTimerC = ttlTimer.C
		}
		nextTime := dueTime
		nextTimer = time.NewTimer(time.Until(nextTime)) // 定时器 , 只执行一次，如果时间是以前，那么现在执行一次
		defer func() {
			if nextTimer.Stop() {
				<-nextTimer.C
			}
			if ttlTimer != nil && ttlTimer.Stop() {
				<-ttlTimerC
			}
		}()
	L:
		for {
			select {
			case <-nextTimer.C:
				// noop
			case <-ttlTimerC:
				// 计时器已经过期，继续删除
				log.Infof("参数为 dueTime: %s, period: %s, TTL: %s, data: %v 的定时器 %s 已经过期。", timerKey, req.DueTime, req.Period, req.TTL, req.Data)
				break L
			case <-stop:
				// 计时器已被删除
				log.Infof("参数为 dueTime: %s, period: %s, TTL: %s, data: %v 的定时器 %s 已被删除。", timerKey, req.DueTime, req.Period, req.TTL, req.Data)
				return
			}

			if _, exists := a.actorsTable.Load(actorKey); exists {
				// 判断对应类型的actor存不存在
				if err = a.executeTimer(req.ActorType, req.ActorID, req.Name, req.DueTime, req.Period, req.Callback, req.Data); err != nil {
					log.Errorf("在actor:%s上调用定时器出错：%s", actorKey, err)
				}
				if repeats > 0 {
					repeats--
				}
			} else {
				log.Errorf("不能找到活跃的定时器 %s", timerKey)
				return
			}
			if repeats == 0 || period == 0 {
				log.Infof("定时器 %s 已完成", timerKey)
				break L
			}
			nextTime = nextTime.Add(period)
			if nextTimer.Stop() {
				<-nextTimer.C
			}
			nextTimer.Reset(time.Until(nextTime))
		}
		err = a.DeleteTimer(ctx, &DeleteTimerRequest{
			Name:      req.Name,
			ActorID:   req.ActorID,
			ActorType: req.ActorType,
		})
		if err != nil {
			log.Errorf("删除定时器出错 %s: %v", timerKey, err)
		}
	}(stop, req)
	return nil
}

// ok
func (a *actorsRuntime) executeTimer(actorType, actorID, name, dueTime, period, callback string, data interface{}) error {
	t := TimerResponse{
		Callback: callback,
		Data:     data,
		DueTime:  dueTime,
		Period:   period,
	}
	b, err := json.Marshal(&t)
	if err != nil {
		return err
	}

	log.Debugf("执行计时器:%s actor类型:%s ID:%s  ", name, actorType, actorID)
	req := invokev1.NewInvokeMethodRequest(fmt.Sprintf("timer/%s", name))
	req.WithActor(actorType, actorID)
	req.WithRawData(b, invokev1.JSONContentType)
	_, err = a.Call(context.Background(), req)
	if err != nil {
		log.Errorf("执行计时器:%s actor类型:%s ID:%s 出错", name, actorType, actorID, err)
	}
	return err
}

// DeleteTimer ok
func (a *actorsRuntime) DeleteTimer(ctx context.Context, req *DeleteTimerRequest) error {
	actorKey := constructCompositeKey(req.ActorType, req.ActorID)
	timerKey := constructCompositeKey(actorKey, req.Name)

	stopChan, exists := a.activeTimers.Load(timerKey)
	if exists {
		close(stopChan.(chan bool))
		a.activeTimers.Delete(timerKey)
	}

	return nil
}
