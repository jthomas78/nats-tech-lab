package refdata

import (
	"context"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

// DefaultContext is the tenant/region context seed data is registered under,
// matching the shipping demo's region.tenant convention (emea/acme).
const DefaultContext = "emea-acme"

type seedItem struct {
	code   string
	name   string // en label
	nameEs string // es label
}

// Seed idempotently registers a representative subset of standard reference
// data (Phase 11.1). Not the full ISO 4217 (~180 currencies) or ISO 3166
// (~249 countries) — a recognizable, demo-sized subset of each, plus the
// complete fixed lists (Incoterms 2020, UN hazard classes, ship-status).
//
// en and es are seeded for every item so the locale-resolution path
// (BR-D03) and the dictionary UI's locale switcher have more than one
// locale to exercise; es is not the seed context's default (en is).
func Seed(ctx context.Context, h *Handlers) error {
	seeds := []struct {
		typeKey     string
		name        string
		description string
		category    domain.TypeCategory
		items       []seedItem
	}{
		{"currency", "Currency", "ISO 4217 currency codes (subset)", domain.CategoryStandards, currencySeed},
		{"country", "Country", "ISO 3166-1 alpha-2 country codes (subset)", domain.CategoryStandards, countrySeed},
		{"incoterm", "Incoterm", "Incoterms 2020 delivery terms", domain.CategoryStandards, incotermSeed},
		{"uom", "Unit of Measure", "UNECE Recommendation 20 unit codes (subset)", domain.CategoryStandards, uomSeed},
		{"hazard-class", "Hazard Class", "UN dangerous goods hazard classes", domain.CategoryStandards, hazardClassSeed},
		{"ship-status", "Ship Status", "AIS navigational status (mirrors backend ShipStatus)", domain.CategoryDomainEnum, shipStatusSeed},
		{"ui-copy", "UI Copy", "Frontend UI chrome strings, sourced as reference data (Phase 11.7)", domain.CategoryUICopy, uiCopySeed},
	}

	if err := h.Localizations.AddLocale(ctx, DefaultContext, "en", true); err != nil {
		return err
	}
	if err := h.Localizations.AddLocale(ctx, DefaultContext, "es", false); err != nil {
		return err
	}

	for _, s := range seeds {
		if err := h.Types.RegisterType(ctx, domain.DictionaryType{
			TypeKey: s.typeKey, Name: s.name, Description: s.description, Category: s.category,
		}); err != nil {
			return err
		}
		for _, item := range s.items {
			_, err := h.Items.RegisterItem(ctx, commands.ItemInput{
				TypeKey: s.typeKey,
				Code:    item.code,
				Context: DefaultContext,
				Attrs:   map[string]any{"name": item.name},
			})
			if err != nil && !errors.Is(err, domain.ErrDuplicateItemCode) {
				return err
			}
			if err := h.Localizations.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: s.typeKey,
				Code:    item.code,
				Context: DefaultContext,
				Locale:  "en",
				Label:   item.name,
			}); err != nil {
				return err
			}
			if err := h.Localizations.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: s.typeKey,
				Code:    item.code,
				Context: DefaultContext,
				Locale:  "es",
				Label:   item.nameEs,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

var currencySeed = []seedItem{
	{"USD", "US Dollar", "Dólar estadounidense"}, {"EUR", "Euro", "Euro"}, {"GBP", "Pound Sterling", "Libra esterlina"}, {"JPY", "Yen", "Yen"},
	{"CNY", "Yuan Renminbi", "Yuan renminbi"}, {"AUD", "Australian Dollar", "Dólar australiano"}, {"CAD", "Canadian Dollar", "Dólar canadiense"},
	{"CHF", "Swiss Franc", "Franco suizo"}, {"HKD", "Hong Kong Dollar", "Dólar de Hong Kong"}, {"NZD", "New Zealand Dollar", "Dólar neozelandés"},
	{"SEK", "Swedish Krona", "Corona sueca"}, {"KRW", "Won", "Won surcoreano"}, {"SGD", "Singapore Dollar", "Dólar de Singapur"}, {"NOK", "Norwegian Krone", "Corona noruega"},
	{"MXN", "Mexican Peso", "Peso mexicano"}, {"INR", "Indian Rupee", "Rupia india"}, {"RUB", "Russian Ruble", "Rublo ruso"}, {"ZAR", "Rand", "Rand sudafricano"},
	{"TRY", "Turkish Lira", "Lira turca"}, {"BRL", "Brazilian Real", "Real brasileño"}, {"TWD", "New Taiwan Dollar", "Nuevo dólar taiwanés"},
	{"DKK", "Danish Krone", "Corona danesa"}, {"PLN", "Zloty", "Zloty polaco"}, {"THB", "Baht", "Baht tailandés"}, {"IDR", "Rupiah", "Rupia indonesia"},
	{"HUF", "Forint", "Forinto húngaro"}, {"CZK", "Czech Koruna", "Corona checa"}, {"ILS", "New Israeli Sheqel", "Nuevo shéquel israelí"},
	{"CLP", "Chilean Peso", "Peso chileno"}, {"PHP", "Philippine Peso", "Peso filipino"}, {"AED", "UAE Dirham", "Dirham de los EAU"},
	{"SAR", "Saudi Riyal", "Rial saudí"}, {"MYR", "Malaysian Ringgit", "Ringgit malayo"}, {"RON", "Romanian Leu", "Leu rumano"}, {"COP", "Colombian Peso", "Peso colombiano"},
}

var countrySeed = []seedItem{
	{"US", "United States", "Estados Unidos"}, {"GB", "United Kingdom", "Reino Unido"}, {"DE", "Germany", "Alemania"}, {"FR", "France", "Francia"},
	{"NL", "Netherlands", "Países Bajos"}, {"BE", "Belgium", "Bélgica"}, {"ES", "Spain", "España"}, {"IT", "Italy", "Italia"}, {"PT", "Portugal", "Portugal"},
	{"SE", "Sweden", "Suecia"}, {"NO", "Norway", "Noruega"}, {"DK", "Denmark", "Dinamarca"}, {"FI", "Finland", "Finlandia"}, {"PL", "Poland", "Polonia"},
	{"CZ", "Czechia", "Chequia"}, {"AT", "Austria", "Austria"}, {"CH", "Switzerland", "Suiza"}, {"IE", "Ireland", "Irlanda"}, {"GR", "Greece", "Grecia"},
	{"TR", "Türkiye", "Turquía"}, {"RU", "Russian Federation", "Federación de Rusia"}, {"CN", "China", "China"}, {"JP", "Japan", "Japón"}, {"KR", "Korea, Republic of", "Corea del Sur"},
	{"IN", "India", "India"}, {"SG", "Singapore", "Singapur"}, {"HK", "Hong Kong", "Hong Kong"}, {"TW", "Taiwan, Province of China", "Taiwán (Provincia de China)"},
	{"TH", "Thailand", "Tailandia"}, {"VN", "Viet Nam", "Vietnam"}, {"ID", "Indonesia", "Indonesia"}, {"MY", "Malaysia", "Malasia"}, {"PH", "Philippines", "Filipinas"},
	{"AU", "Australia", "Australia"}, {"NZ", "New Zealand", "Nueva Zelanda"}, {"ZA", "South Africa", "Sudáfrica"}, {"AE", "United Arab Emirates", "Emiratos Árabes Unidos"},
	{"SA", "Saudi Arabia", "Arabia Saudita"}, {"IL", "Israel", "Israel"}, {"EG", "Egypt", "Egipto"}, {"NG", "Nigeria", "Nigeria"}, {"KE", "Kenya", "Kenia"},
	{"BR", "Brazil", "Brasil"}, {"AR", "Argentina", "Argentina"}, {"CL", "Chile", "Chile"}, {"CO", "Colombia", "Colombia"}, {"MX", "Mexico", "México"},
	{"CA", "Canada", "Canadá"}, {"PA", "Panama", "Panamá"}, {"UA", "Ukraine", "Ucrania"}, {"RO", "Romania", "Rumania"}, {"HU", "Hungary", "Hungría"},
}

var incotermSeed = []seedItem{
	{"EXW", "Ex Works", "En fábrica"}, {"FCA", "Free Carrier", "Franco transportista"}, {"CPT", "Carriage Paid To", "Transporte pagado hasta"},
	{"CIP", "Carriage and Insurance Paid To", "Transporte y seguro pagados hasta"}, {"DAP", "Delivered at Place", "Entregada en lugar"},
	{"DPU", "Delivered at Place Unloaded", "Entregada en lugar descargada"}, {"DDP", "Delivered Duty Paid", "Entregada derechos pagados"},
	{"FAS", "Free Alongside Ship", "Franco al costado del buque"}, {"FOB", "Free on Board", "Franco a bordo"},
	{"CFR", "Cost and Freight", "Costo y flete"}, {"CIF", "Cost, Insurance and Freight", "Costo, seguro y flete"},
}

var uomSeed = []seedItem{
	{"KGM", "Kilogram", "Kilogramo"}, {"GRM", "Gram", "Gramo"}, {"TNE", "Tonne", "Tonelada"}, {"MTR", "Metre", "Metro"},
	{"MTK", "Square Metre", "Metro cuadrado"}, {"MTQ", "Cubic Metre", "Metro cúbico"}, {"LTR", "Litre", "Litro"}, {"PCE", "Piece", "Pieza"},
	{"HUR", "Hour", "Hora"}, {"DAY", "Day", "Día"}, {"KMH", "Kilometre per Hour", "Kilómetro por hora"}, {"CEL", "Degree Celsius", "Grado Celsius"},
}

var hazardClassSeed = []seedItem{
	{"1", "Explosives", "Explosivos"},
	{"2", "Gases", "Gases"},
	{"3", "Flammable Liquids", "Líquidos inflamables"},
	{"4", "Flammable Solids", "Sólidos inflamables"},
	{"5", "Oxidizing Substances and Organic Peroxides", "Sustancias comburentes y peróxidos orgánicos"},
	{"6", "Toxic and Infectious Substances", "Sustancias tóxicas e infecciosas"},
	{"7", "Radioactive Material", "Material radiactivo"},
	{"8", "Corrosive Substances", "Sustancias corrosivas"},
	{"9", "Miscellaneous Dangerous Goods", "Mercancías peligrosas varias"},
}

// shipStatusSeed mirrors backend/dictionary/internal/domain/ship.go's
// ShipStatus constants by value — this module has no dependency on the
// shipping backend's code, so the codes are duplicated here as plain
// strings rather than imported.
var shipStatusSeed = []seedItem{
	{"in-transit", "In Transit", "En tránsito"},
	{"docked", "Docked", "Atracado"},
	{"at-anchor", "At Anchor", "Fondeado"},
	{"not-under-command", "Not Under Command", "Sin gobierno"},
	{"restricted-manoeuvrability", "Restricted Manoeuvrability", "Maniobrabilidad restringida"},
}

// uiCopySeed is UI chrome copy (Phase 11.7) — codes are vue-i18n message
// keys, not domain codes; labels are the copy itself. Deliberately small:
// this seeds the two strings the shipping frontends actually adopt this
// phase (the status-filter "All" option and the topbar "Language" label),
// not a full i18n catalog.
var uiCopySeed = []seedItem{
	{"filter.all", "All", "Todos"},
	{"nav.language", "Language", "Idioma"},
}
