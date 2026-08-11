package vat

const INTROSPECTION_QUERYTYPE string = "Query"

const VERSION VatContextKey = "VAT_VERSION"
const VECTR_VERSION VatContextKey = "VAT_VECTR_VERSION"

// PLACEHOLDER_DEFENSE_LAYER_NAME is assigned to a defense tool ref that
// arrives with zero defense layers. VECTR's create/update API rejects an
// empty DefenseLayerIds list, but some source instances genuinely hold
// tools in that state -- a prior VECTR migration allowed it even though
// VECTR itself no longer does. Rather than failing the whole restore over
// it, the tool gets this obviously-fake layer attached and a warning is
// logged, so the tool lands in the target instance and the gap is visible
// for a human to go fix.
const PLACEHOLDER_DEFENSE_LAYER_NAME string = "NEEDS REVIEW - NO LAYER ASSIGNED"
