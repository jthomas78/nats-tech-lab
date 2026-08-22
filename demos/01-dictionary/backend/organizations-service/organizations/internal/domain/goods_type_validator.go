package domain

import "context"

// GoodsTypeValidator is BR-TP64's tenant-scoped refdata port. 39a depends on
// this port and its fake; 39b supplies the goods-type vocabulary and its live
// corpus. Keeping the lookup outside ComplianceDocument follows BR-TP14's
// established refdataclient pattern.
type GoodsTypeValidator interface {
	GoodsTypeExists(ctx context.Context, tenant, contextKey, code string) (bool, error)
}
