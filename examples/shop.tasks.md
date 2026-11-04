# Tasks for examples/shop.smap.
# Run: sysmap serve examples/shop.smap --tasks examples/shop.tasks.md
#
# To see the missing-task warning, run check against examples/shop-plan.smap:
#   sysmap check examples/shop-plan.smap --tasks examples/shop.tasks.md
# `refund` is marked + there but has no task.
- [x] ledger: post refunds as a reversal entry
- [~] checkout: expose the retry path in the storefront
- [x] order_store: record the refunded order state
- [ ] refund_policy: decide who may refund a shipment in transit
- [ ] stock: release the reservation after approval
