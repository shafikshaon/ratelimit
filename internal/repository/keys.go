package repository

import "fmt"

const (
	tierCachePrefix      = "rl:config:"
	overrideCachePrefix  = "rl:override:"
	overrideNullSentinel = "null"
)

func overrideCacheKey(apiName, wallet string) string {
	return fmt.Sprintf("%s%s:%s", overrideCachePrefix, apiName, wallet)
}
