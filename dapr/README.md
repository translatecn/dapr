### Makefile
```

PROTOS = operator placement sentry common runtime internals

define genProtoForTarget
.PHONY: $(1)
protoc-gen-$(1)-v1:
	protoc --go_out=. --go_opt=module=github.com/dapr/dapr --go-grpc_out=. \
      --go-grpc_opt=require_unimplemented_servers=false,module=github.com/dapr/dapr ./dapr/proto/$(1)/v1/*.proto
endef

# Generate proto gen targets
$(foreach ITEM,$(PROTOS),$(eval $(call genProtoForTarget,$(ITEM))))

PROTOC_ALL_TARGETS:=$(foreach ITEM,$(PROTOS),protoc-gen-$(ITEM)-v1)

protoc-gen: $(PROTOC_ALL_TARGETS)
```
然后 pkg/proto下就会生成对应的go文件