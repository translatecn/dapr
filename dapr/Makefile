
PROTOS = operator placement sentry common runtime internals


define genProtoForTarget
.PHONY: $(1)
protoc-gen-$(1)-v1:
	echo $(1)
endef

# Generate proto gen targets
$(foreach ITEM,$(PROTOS),$(eval $(call genProtoForTarget,$(ITEM))))

PROTOC_ALL_TARGETS:=$(foreach ITEM,$(PROTOS),protoc-gen-$(ITEM)-v1)

protoc-gen: $(PROTOC_ALL_TARGETS)