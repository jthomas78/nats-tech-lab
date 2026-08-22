package main

// goodsTypeSeed is a representative placeholder derived conceptually from
// V2's commodityCategoryEntities. It is intentionally not an extracted or
// authoritative commodity taxonomy: Phase 39 captures and reports cover but
// does not allocate loads against it, so this corpus only needs enough breadth
// to exercise GIT certificate entry. Replace it when the tier-1 commodity-
// taxonomy extraction lands. It is flat by design and has no invented
// hierarchy. Not wired into refdata-service's Seed() — see main.go.
var goodsTypeSeed = []goodsTypeRow{
	{Code: "GENERAL_FREIGHT", Label: "General freight", Description: "Mixed packaged goods that do not require a specialised commodity category."},
	{Code: "PALLETISED_GOODS", Label: "Palletised goods", Description: "Goods consolidated onto pallets for handling and road transport."},
	{Code: "REFRIGERATED_FOODSTUFFS", Label: "Refrigerated foodstuffs", Description: "Temperature-controlled food products requiring refrigerated transport."},
	{Code: "FRESH_PRODUCE", Label: "Fresh produce", Description: "Fresh fruit, vegetables and other perishable agricultural produce."},
	{Code: "DRY_BULK", Label: "Dry bulk", Description: "Loose dry commodities such as grain, minerals, powders or aggregates."},
	{Code: "LIQUID_BULK", Label: "Liquid bulk", Description: "Bulk liquid commodities transported in tanks or dedicated containers."},
	{Code: "HAZARDOUS_MATERIALS", Label: "Hazardous materials", Description: "Regulated dangerous goods requiring specialised handling and carriage."},
	{Code: "LIVESTOCK", Label: "Livestock", Description: "Live animals transported under animal-welfare and biosecurity controls."},
	{Code: "HIGH_VALUE_GOODS", Label: "High-value goods", Description: "Goods whose value or theft exposure requires enhanced security controls."},
	{Code: "ABNORMAL_LOAD", Label: "Abnormal load", Description: "Oversized or overweight goods requiring permits and specialised transport."},
}
