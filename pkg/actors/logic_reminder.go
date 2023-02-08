package actors

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/dapr/components-contrib/state"
	"github.com/dapr/dapr/pkg/concurrency"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"reflect"
	"sync"
	"time"
)

// 将元信息 合并到actor type中
func (a *actorsRuntime) migrateRemindersForActorType(actorType string, actorMetadata *ActorMetadata) (*ActorMetadata, error) {
	if !a.actorTypeMetadataEnabled {
		return actorMetadata, nil
	}

	// 如果元信息的分区数、与全局配置的分区数一致
	if actorMetadata.RemindersMetadata.PartitionCount == a.config.RemindersStoragePartitions {
		return actorMetadata, nil
	}

	if actorMetadata.RemindersMetadata.PartitionCount > a.config.RemindersStoragePartitions {
		log.Warnf("不能减少reminder分区数量%s", actorType)
		return actorMetadata, nil
	}

	log.Warnf("迁移reminder元数据记录 %s", actorType)

	// 取出所有reminder
	reminderRefs, refreshedActorMetadata, err := a.getRemindersForActorType(actorType, false)
	if err != nil {
		return nil, err
	}
	if refreshedActorMetadata.ID != actorMetadata.ID {
		return nil, errors.Errorf("could not migrate reminders for actor type %s due to race condition in actor metadata", actorType)
	}

	// Recreate as a new metadata identifier.
	actorMetadata.ID = uuid.NewString()
	actorMetadata.RemindersMetadata.PartitionCount = a.config.RemindersStoragePartitions
	actorRemindersPartitions := make([][]*Reminder, actorMetadata.RemindersMetadata.PartitionCount)
	for i := 0; i < actorMetadata.RemindersMetadata.PartitionCount; i++ {
		actorRemindersPartitions[i] = make([]*Reminder, 0)
	}

	// 为每个reminder 重新计算分区。
	for _, reminderRef := range reminderRefs {
		partitionID := actorMetadata.calculateReminderPartition(reminderRef.reminder.ActorID, reminderRef.reminder.Name)
		actorRemindersPartitions[partitionID-1] = append(actorRemindersPartitions[partitionID-1], reminderRef.reminder)
	}

	// Save to database.
	metadata := map[string]string{metadataPartitionKey: actorMetadata.ID}
	transaction := state.TransactionalStateRequest{
		Metadata: metadata,
	}
	for i := 0; i < actorMetadata.RemindersMetadata.PartitionCount; i++ {
		partitionID := i + 1
		stateKey := actorMetadata.calculateRemindersStateKey(actorType, uint32(partitionID))
		stateValue := actorRemindersPartitions[i]
		transaction.Operations = append(transaction.Operations, state.TransactionalStateOperation{
			Operation: state.Upsert,
			Request: state.SetRequest{
				Key:      stateKey,
				Value:    stateValue,
				Metadata: metadata,
			},
		})
	}
	err = a.transactionalStore.Multi(&transaction)
	if err != nil {
		return nil, err
	}

	// 保存新的元数据，以便新的“metadataID”成为reminder的新因素引用列表。
	err = a.saveActorTypeMetadata(actorType, actorMetadata)
	if err != nil {
		return nil, err
	}
	log.Warnf(
		"完成actor类型%s的actor元数据记录迁移，新的元数据ID=%s",
		actorType, actorMetadata.ID)
	return actorMetadata, nil
}

// 根据actor type 获取reminders
func (a *actorsRuntime) getRemindersForActorType(actorType string, migrate bool) ([]actorReminderReference, *ActorMetadata, error) {
	if a.store == nil {
		return nil, nil, errors.New("actors: 状态存储不存在或配置不正确")
	}

	actorMetadata, merr := a.getActorTypeMetadata(actorType, migrate)
	if merr != nil {
		return nil, nil, fmt.Errorf("不能读取actor type元数据: %w", merr)
	}

	if actorMetadata.RemindersMetadata.PartitionCount >= 1 {
		metadata := map[string]string{metadataPartitionKey: actorMetadata.ID}
		actorMetadata.RemindersMetadata.partitionsEtag = map[uint32]*string{}
		var reminders []actorReminderReference

		keyPartitionMap := map[string]uint32{}
		var getRequests []state.GetRequest
		for i := 1; i <= actorMetadata.RemindersMetadata.PartitionCount; i++ {
			partition := uint32(i)
			key := actorMetadata.calculateRemindersStateKey(actorType, partition)
			keyPartitionMap[key] = partition
			getRequests = append(getRequests, state.GetRequest{
				Key:      key,
				Metadata: metadata,
			})
		}

		bulkGet, bulkResponse, err := a.store.BulkGet(getRequests)
		if bulkGet {
			if err != nil {
				return nil, nil, err
			}
		} else {
			// TODO(artursouza): refactor this fallback into default implementation in contrib.
			// if store doesn't support bulk get, fallback to call get() method one by one
			limiter := concurrency.NewLimiter(actorMetadata.RemindersMetadata.PartitionCount)
			bulkResponse = make([]state.BulkGetResponse, len(getRequests))
			for i := range getRequests {
				getRequest := getRequests[i]
				bulkResponse[i].Key = getRequest.Key

				fn := func(param interface{}) {
					r := param.(*state.BulkGetResponse)
					resp, ferr := a.store.Get(&getRequest)
					if ferr != nil {
						r.Error = ferr.Error()
					} else if resp != nil {
						r.Data = jsoniter.RawMessage(resp.Data)
						r.ETag = resp.ETag
						r.Metadata = resp.Metadata
					}
				}

				limiter.Execute(fn, &bulkResponse[i])
			}
			limiter.Wait()
		}

		for _, resp := range bulkResponse {
			partition := keyPartitionMap[resp.Key]
			actorMetadata.RemindersMetadata.partitionsEtag[partition] = resp.ETag
			if resp.Error != "" {
				return nil, nil, fmt.Errorf("could not get reminders partition %v: %v", resp.Key, resp.Error)
			}

			var batch []Reminder
			if len(resp.Data) > 0 {
				err = json.Unmarshal(resp.Data, &batch)
				if err != nil {
					return nil, nil, fmt.Errorf("could not parse actor reminders partition %v: %w", resp.Key, err)
				}
			}

			for j := range batch {
				reminders = append(reminders, actorReminderReference{
					actorMetadataID:           actorMetadata.ID,
					actorRemindersPartitionID: partition,
					reminder:                  &batch[j],
				})
			}
		}

		return reminders, actorMetadata, nil
	}

	key := constructCompositeKey("actors", actorType)
	resp, err := a.store.Get(&state.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, nil, err
	}

	var reminders []Reminder
	if len(resp.Data) > 0 {
		err = json.Unmarshal(resp.Data, &reminders)
		if err != nil {
			return nil, nil, fmt.Errorf("could not parse actor reminders: %v", err)
		}
	}

	reminderRefs := make([]actorReminderReference, len(reminders))
	for j := range reminders {
		reminderRefs[j] = actorReminderReference{
			actorMetadataID:           "",
			actorRemindersPartitionID: 0,
			reminder:                  &reminders[j],
		}
	}

	actorMetadata.RemindersMetadata.partitionsEtag = map[uint32]*string{
		0: resp.ETag,
	}
	return reminderRefs, actorMetadata, nil
}

func (a *actorsRuntime) saveRemindersInPartition(ctx context.Context, stateKey string, reminders []Reminder, etag *string, databasePartitionKey string) error {
	// Even when data is not partitioned, the save operation is the same.
	// The only difference is stateKey.
	return a.store.Set(&state.SetRequest{
		Key:      stateKey,
		Value:    reminders,
		ETag:     etag,
		Metadata: map[string]string{metadataPartitionKey: databasePartitionKey},
	})
}

// DeleteReminder OK
func (a *actorsRuntime) DeleteReminder(ctx context.Context, req *DeleteReminderRequest) error {
	if a.store == nil {
		return errors.New("actors: 状态存储不存在或配置不正确")
	}

	if a.evaluationBusy {
		select {
		case <-time.After(time.Second * 5):
			return errors.New("删除reminder 失败:5秒超时")
		case <-a.evaluationChan:
			break
		}
	}

	actorKey := constructCompositeKey(req.ActorType, req.ActorID)
	reminderKey := constructCompositeKey(actorKey, req.Name)

	stop, exists := a.activeReminders.Load(reminderKey)
	if exists {
		log.Infof("发现reminder: %v. 删除reminder", reminderKey)
		close(stop.(chan bool))
		a.activeReminders.Delete(reminderKey)
	}

	err := backoff.Retry(func() error {
		reminders, actorMetadata, err := a.getRemindersForActorType(req.ActorType, true)
		if err != nil {
			return err
		}

		// remove from partition first.
		remindersInPartition, stateKey, etag := actorMetadata.removeReminderFromPartition(reminders, req.ActorType, req.ActorID, req.Name)

		// now, we can remove from the "global" list.
		for i := len(reminders) - 1; i >= 0; i-- {
			if reminders[i].reminder.ActorType == req.ActorType && reminders[i].reminder.ActorID == req.ActorID && reminders[i].reminder.Name == req.Name {
				reminders = append(reminders[:i], reminders[i+1:]...)
			}
		}

		// Get the database partiton key (needed for CosmosDB)
		databasePartitionKey := actorMetadata.calculateDatabasePartitionKey(stateKey)

		// Then, save the partition to the database.
		err = a.saveRemindersInPartition(ctx, stateKey, remindersInPartition, etag, databasePartitionKey)
		if err != nil {
			return err
		}

		// Finally, we must save metadata to get a new eTag.
		// This avoids a race condition between an update and a repartitioning.
		a.saveActorTypeMetadata(req.ActorType, actorMetadata)

		a.remindersLock.Lock()
		a.reminders[req.ActorType] = reminders
		a.remindersLock.Unlock()
		return nil
	}, backoff.NewExponentialBackOff())
	if err != nil {
		return err
	}

	err = a.store.Delete(&state.DeleteRequest{
		Key: reminderKey,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetReminder ok
func (a *actorsRuntime) GetReminder(ctx context.Context, req *GetReminderRequest) (*Reminder, error) {
	reminders, _, err := a.getRemindersForActorType(req.ActorType, true)
	if err != nil {
		return nil, err
	}

	for _, r := range reminders {
		if r.reminder.ActorID == req.ActorID && r.reminder.Name == req.Name {
			return &Reminder{
				Data:    r.reminder.Data,
				DueTime: r.reminder.DueTime,
				Period:  r.reminder.Period,
			}, nil
		}
	}
	return nil, nil
}

//  ok
func (a *actorsRuntime) getReminderTrack(actorKey, name string) (*ReminderTrack, error) {
	if a.store == nil {
		return nil, errors.New("actors: 状态存储不存在或配置不正确")
	}

	resp, err := a.store.Get(&state.GetRequest{
		Key: constructCompositeKey(actorKey, name),
	})
	if err != nil {
		return nil, err
	}

	track := ReminderTrack{
		RepetitionLeft: -1,
	}
	json.Unmarshal(resp.Data, &track)
	return &track, nil
}

// ok
func (a *actorsRuntime) updateReminderTrack(actorKey, name string, repetition int, lastInvokeTime time.Time) error {
	if a.store == nil {
		return errors.New("actors: 状态存储不存在或配置不正确")
	}

	track := ReminderTrack{
		LastFiredTime:  lastInvokeTime.Format(time.RFC3339),
		RepetitionLeft: repetition,
	}

	err := a.store.Set(&state.SetRequest{
		Key:   constructCompositeKey(actorKey, name),
		Value: track,
	})
	return err
}

// ok
func (a *actorsRuntime) startReminder(reminder *Reminder, stopChannel chan bool) error { // 启动reminder
	actorKey := constructCompositeKey(reminder.ActorType, reminder.ActorID)
	reminderKey := constructCompositeKey(actorKey, reminder.Name)

	var (
		nextTime, ttl            time.Time
		period                   time.Duration
		repeats, repetitionsLeft int
	)
	// 获取注册时间
	registeredTime, err := time.Parse(time.RFC3339, reminder.RegisteredTime)
	if err != nil {
		return errors.Wrap(err, "解析reminder注册时间失败")
	}
	if len(reminder.ExpirationTime) != 0 {
		if ttl, err = time.Parse(time.RFC3339, reminder.ExpirationTime); err != nil {
			return errors.Wrap(err, "解析reminder过期时间失败")
		}
	}

	repeats = -1 // set to default
	if len(reminder.Period) != 0 {
		if period, repeats, err = parseDuration(reminder.Period); err != nil {
			return errors.Wrap(err, "解析reminder周期失败")
		}
	}
	//是一个持久化的对象，它记录了最后一次提醒的时间。
	track, err := a.getReminderTrack(actorKey, reminder.Name)
	if err != nil {
		return errors.Wrap(err, "获取reminder执行信息失败")
	}

	if track != nil && len(track.LastFiredTime) != 0 {
		lastFiredTime, err := time.Parse(time.RFC3339, track.LastFiredTime)
		if err != nil {
			return errors.Wrap(err, "获取reminder上一次执行时间失败")
		}
		repetitionsLeft = track.RepetitionLeft
		nextTime = lastFiredTime.Add(period)
	} else {
		repetitionsLeft = repeats
		nextTime = registeredTime
	}

	go func(reminder *Reminder, period time.Duration, nextTime, ttl time.Time, repetitionsLeft int, stop chan bool) {
		var (
			ttlTimer, nextTimer *time.Timer
			ttlTimerC           <-chan time.Time
			err                 error
		)
		// 不是零值
		if !ttl.IsZero() {
			ttlTimer = time.NewTimer(time.Until(ttl))
			ttlTimerC = ttlTimer.C
		}
		nextTimer = time.NewTimer(time.Until(nextTime))
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
			case v := <-nextTimer.C:
				log.Debug(v)
				// noop
			case <-ttlTimerC:
				// 继续删除提醒信息
				log.Infof("reminder %s 过期了", reminder.Name)
				break L
			case <-stop:
				// reminder 被删除
				log.Infof("reminder %s with parameters: dueTime: %s, period: %s, data: %v 被删除了", reminder.Name, reminder.RegisteredTime, reminder.Period, reminder.Data)
				return
			}

			_, exists := a.activeReminders.Load(reminderKey)
			if !exists {
				log.Errorf("不能查找到活跃的 reminder  by key: %s", reminderKey)
				return
			}
			// 如果所有的重复都已完成，则继续进行提醒删除。
			if repetitionsLeft == 0 {
				log.Infof("reminder %q 完成了 %d ", reminder.Name, repeats)
				break L
			}
			if err = a.executeReminder(reminder); err != nil {
				log.Errorf("error 执行reminder %q ;actor type %s; id %s; err: %v",
					reminder.Name, reminder.ActorType, reminder.ActorID, err)
			}
			if repetitionsLeft > 0 {
				repetitionsLeft--
			}
			if err = a.updateReminderTrack(actorKey, reminder.Name, repetitionsLeft, nextTime); err != nil {
				log.Errorf("更新reminder执行信息失败 %v", err)
			}
			// 如果reminder不是重复的，则继续删除reminder
			if period == 0 {
				break L
			}
			nextTime = nextTime.Add(period)
			if nextTimer.Stop() {
				<-nextTimer.C
			}
			nextTimer.Reset(time.Until(nextTime))
		}
		err = a.DeleteReminder(context.TODO(), &DeleteReminderRequest{
			Name:      reminder.Name,
			ActorID:   reminder.ActorID,
			ActorType: reminder.ActorType,
		})
		if err != nil {
			log.Errorf("error deleting reminder: %s", err)
		}
	}(reminder, period, nextTime, ttl, repetitionsLeft, stopChannel)

	return nil
}

//type Reminder struct {
//	ActorID        actorId-a
//	ActorType      actorType-a
//	Name           demo
//	RegisteredTime 2022-01-04T14:35:37+08:00
//}
// 执行某个reminder
func (a *actorsRuntime) executeReminder(reminder *Reminder) error {
	r := ReminderResponse{
		DueTime: reminder.DueTime,
		Period:  reminder.Period,
		Data:    reminder.Data,
	}
	b, err := json.Marshal(&r)
	if err != nil {
		return err
	}

	log.Debugf("执行 reminder %s ;actor type %s ; id %s", reminder.Name, reminder.ActorType, reminder.ActorID)
	req := invokev1.NewInvokeMethodRequest(fmt.Sprintf("remind/%s", reminder.Name))
	req.WithActor(reminder.ActorType, reminder.ActorID)
	req.WithRawData(b, invokev1.JSONContentType)

	_, err = a.callLocalActor(context.Background(), req)
	return err
}

// ok
func (a *actorsRuntime) reminderRequiresUpdate(req *CreateReminderRequest, reminder *Reminder) bool {
	if reminder.ActorID == req.ActorID && reminder.ActorType == req.ActorType && reminder.Name == req.Name &&
		(!reflect.DeepEqual(reminder.Data, req.Data) || reminder.DueTime != req.DueTime || reminder.Period != req.Period ||
			len(req.TTL) != 0 || (len(reminder.ExpirationTime) != 0 && len(req.TTL) == 0)) {
		return true
	}

	return false
}

// ok
func (a *actorsRuntime) getReminder(req *CreateReminderRequest) (*Reminder, bool) {
	a.remindersLock.RLock()
	reminders := a.reminders[req.ActorType] // 用户随便写
	a.remindersLock.RUnlock()
	//判断有没有已经存在的 reminder
	for _, r := range reminders {
		if r.reminder.ActorID == req.ActorID && r.reminder.ActorType == req.ActorType && r.reminder.Name == req.Name {
			return r.reminder, true
		}
	}

	return nil, false
}

//评估提示
func (a *actorsRuntime) evaluateReminders() {
	a.evaluationLock.Lock()
	defer a.evaluationLock.Unlock()

	a.evaluationBusy = true
	a.evaluationChan = make(chan bool)

	var wg sync.WaitGroup
	for _, t := range a.config.HostedActorTypes {
		vals, _, err := a.getRemindersForActorType(t, true)
		if err != nil {
			log.Errorf("error getting reminders for actor type %s: %s", t, err)
		} else {
			a.remindersLock.Lock()
			a.reminders[t] = vals
			a.remindersLock.Unlock()

			wg.Add(1)
			go func(wg *sync.WaitGroup, reminders []actorReminderReference) {
				defer wg.Done()

				for i := range reminders {
					r := reminders[i] // Make a copy since we will refer to this as a reference in this loop.
					targetActorAddress, _ := a.placement.LookupActor(r.reminder.ActorType, r.reminder.ActorID)
					if targetActorAddress == "" {
						continue
					}

					if a.isActorLocal(targetActorAddress, a.config.HostAddress, a.config.Port) {
						actorKey := constructCompositeKey(r.reminder.ActorType, r.reminder.ActorID)
						reminderKey := constructCompositeKey(actorKey, r.reminder.Name)
						_, exists := a.activeReminders.Load(reminderKey)

						if !exists {
							stop := make(chan bool)
							a.activeReminders.Store(reminderKey, stop)
							err := a.startReminder(r.reminder, stop)
							if err != nil {
								log.Errorf("error starting reminder: %s", err)
							}
						}
					}
				}
			}(&wg, vals)
		}
	}
	wg.Wait()
	close(a.evaluationChan)
	a.evaluationBusy = false
}
