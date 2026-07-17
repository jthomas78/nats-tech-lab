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
	nameAf string // af-za label
}

// Seed idempotently registers a representative subset of standard reference
// data (Phase 11.1). Not the full ISO 4217 (~180 currencies) or ISO 3166
// (~249 countries) — a recognizable, demo-sized subset of each, plus the
// complete fixed lists (Incoterms 2020, UN hazard classes, ship-status).
//
// en, es, and af-za are seeded for every item so the locale-resolution path
// (BR-D03) and the dictionary UI's locale switcher have more than one
// locale to exercise; es and af-za are not the seed context's default (en is).
// Like es, the af-za labels are a machine-drafted first pass, not a
// human/translator-reviewed deliverable. Locale codes are lower case
// throughout (BR-D20) — af-za, not the BCP-47-conventional af-ZA.
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
	if err := h.Localizations.AddLocale(ctx, DefaultContext, "af-za", false); err != nil {
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
			if err := h.Localizations.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: s.typeKey,
				Code:    item.code,
				Context: DefaultContext,
				Locale:  "af-za",
				Label:   item.nameAf,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

var currencySeed = []seedItem{
	{"USD", "US Dollar", "Dólar estadounidense", "Amerikaanse dollar"},
	{"EUR", "Euro", "Euro", "Euro"},
	{"GBP", "Pound Sterling", "Libra esterlina", "Pond sterling"},
	{"JPY", "Yen", "Yen", "Jen"},
	{"CNY", "Yuan Renminbi", "Yuan renminbi", "Yuan-renminbi"},
	{"AUD", "Australian Dollar", "Dólar australiano", "Australiese dollar"},
	{"CAD", "Canadian Dollar", "Dólar canadiense", "Kanadese dollar"},
	{"CHF", "Swiss Franc", "Franco suizo", "Switserse frank"},
	{"HKD", "Hong Kong Dollar", "Dólar de Hong Kong", "Hongkong-dollar"},
	{"NZD", "New Zealand Dollar", "Dólar neozelandés", "Nieu-Seelandse dollar"},
	{"SEK", "Swedish Krona", "Corona sueca", "Sweedse kroon"},
	{"KRW", "Won", "Won surcoreano", "Suid-Koreaanse won"},
	{"SGD", "Singapore Dollar", "Dólar de Singapur", "Singapoerse dollar"},
	{"NOK", "Norwegian Krone", "Corona noruega", "Noorse kroon"},
	{"MXN", "Mexican Peso", "Peso mexicano", "Meksikaanse peso"},
	{"INR", "Indian Rupee", "Rupia india", "Indiese roepie"},
	{"RUB", "Russian Ruble", "Rublo ruso", "Russiese roebel"},
	{"ZAR", "Rand", "Rand sudafricano", "Rand"},
	{"TRY", "Turkish Lira", "Lira turca", "Turkse lira"},
	{"BRL", "Brazilian Real", "Real brasileño", "Braziliaanse real"},
	{"TWD", "New Taiwan Dollar", "Nuevo dólar taiwanés", "Nuwe Taiwanese dollar"},
	{"DKK", "Danish Krone", "Corona danesa", "Deense kroon"},
	{"PLN", "Zloty", "Zloty polaco", "Poolse zloty"},
	{"THB", "Baht", "Baht tailandés", "Thaise baht"},
	{"IDR", "Rupiah", "Rupia indonesia", "Indonesiese roepia"},
	{"HUF", "Forint", "Forinto húngaro", "Hongaarse forint"},
	{"CZK", "Czech Koruna", "Corona checa", "Tsjeggiese kroon"},
	{"ILS", "New Israeli Sheqel", "Nuevo shéquel israelí", "Nuwe Israeliese sikkel"},
	{"CLP", "Chilean Peso", "Peso chileno", "Chileense peso"},
	{"PHP", "Philippine Peso", "Peso filipino", "Filippynse peso"},
	{"AED", "UAE Dirham", "Dirham de los EAU", "VAE-dirham"},
	{"SAR", "Saudi Riyal", "Rial saudí", "Saoedi-riyal"},
	{"MYR", "Malaysian Ringgit", "Ringgit malayo", "Maleisiese ringgit"},
	{"RON", "Romanian Leu", "Leu rumano", "Roemeense leu"},
	{"COP", "Colombian Peso", "Peso colombiano", "Colombiaanse peso"},
}

var countrySeed = []seedItem{
	{"US", "United States", "Estados Unidos", "Verenigde State"},
	{"GB", "United Kingdom", "Reino Unido", "Verenigde Koninkryk"},
	{"DE", "Germany", "Alemania", "Duitsland"},
	{"FR", "France", "Francia", "Frankryk"},
	{"NL", "Netherlands", "Países Bajos", "Nederland"},
	{"BE", "Belgium", "Bélgica", "België"},
	{"ES", "Spain", "España", "Spanje"},
	{"IT", "Italy", "Italia", "Italië"},
	{"PT", "Portugal", "Portugal", "Portugal"},
	{"SE", "Sweden", "Suecia", "Swede"},
	{"NO", "Norway", "Noruega", "Noorweë"},
	{"DK", "Denmark", "Dinamarca", "Denemarke"},
	{"FI", "Finland", "Finlandia", "Finland"},
	{"PL", "Poland", "Polonia", "Pole"},
	{"CZ", "Czechia", "Chequia", "Tsjeggië"},
	{"AT", "Austria", "Austria", "Oostenryk"},
	{"CH", "Switzerland", "Suiza", "Switserland"},
	{"IE", "Ireland", "Irlanda", "Ierland"},
	{"GR", "Greece", "Grecia", "Griekeland"},
	{"TR", "Türkiye", "Turquía", "Turkye"},
	{"RU", "Russian Federation", "Federación de Rusia", "Russiese Federasie"},
	{"CN", "China", "China", "China"},
	{"JP", "Japan", "Japón", "Japan"},
	{"KR", "Korea, Republic of", "Corea del Sur", "Suid-Korea"},
	{"IN", "India", "India", "Indië"},
	{"SG", "Singapore", "Singapur", "Singapoer"},
	{"HK", "Hong Kong", "Hong Kong", "Hongkong"},
	{"TW", "Taiwan, Province of China", "Taiwán (Provincia de China)", "Taiwan (Provinsie van China)"},
	{"TH", "Thailand", "Tailandia", "Thailand"},
	{"VN", "Viet Nam", "Vietnam", "Viëtnam"},
	{"ID", "Indonesia", "Indonesia", "Indonesië"},
	{"MY", "Malaysia", "Malasia", "Maleisië"},
	{"PH", "Philippines", "Filipinas", "Filippyne"},
	{"AU", "Australia", "Australia", "Australië"},
	{"NZ", "New Zealand", "Nueva Zelanda", "Nieu-Seeland"},
	{"ZA", "South Africa", "Sudáfrica", "Suid-Afrika"},
	{"AE", "United Arab Emirates", "Emiratos Árabes Unidos", "Verenigde Arabiese Emirate"},
	{"SA", "Saudi Arabia", "Arabia Saudita", "Saoedi-Arabië"},
	{"IL", "Israel", "Israel", "Israel"},
	{"EG", "Egypt", "Egipto", "Egipte"},
	{"NG", "Nigeria", "Nigeria", "Nigerië"},
	{"KE", "Kenya", "Kenia", "Kenia"},
	{"BR", "Brazil", "Brasil", "Brasilië"},
	{"AR", "Argentina", "Argentina", "Argentinië"},
	{"CL", "Chile", "Chile", "Chili"},
	{"CO", "Colombia", "Colombia", "Colombia"},
	{"MX", "Mexico", "México", "Mexiko"},
	{"CA", "Canada", "Canadá", "Kanada"},
	{"PA", "Panama", "Panamá", "Panama"},
	{"UA", "Ukraine", "Ucrania", "Oekraïne"},
	{"RO", "Romania", "Rumania", "Roemenië"},
	{"HU", "Hungary", "Hungría", "Hongarye"},
}

var incotermSeed = []seedItem{
	{"EXW", "Ex Works", "En fábrica", "Af Fabriek"},
	{"FCA", "Free Carrier", "Franco transportista", "Vry Vervoerder"},
	{"CPT", "Carriage Paid To", "Transporte pagado hasta", "Vervoer Betaal Tot"},
	{"CIP", "Carriage and Insurance Paid To", "Transporte y seguro pagados hasta", "Vervoer en Versekering Betaal Tot"},
	{"DAP", "Delivered at Place", "Entregada en lugar", "Afgelewer by Plek"},
	{"DPU", "Delivered at Place Unloaded", "Entregada en lugar descargada", "Afgelewer by Plek Afgelaai"},
	{"DDP", "Delivered Duty Paid", "Entregada derechos pagados", "Afgelewer Reg Betaal"},
	{"FAS", "Free Alongside Ship", "Franco al costado del buque", "Vry Langs Skip"},
	{"FOB", "Free on Board", "Franco a bordo", "Vry aan Boord"},
	{"CFR", "Cost and Freight", "Costo y flete", "Koste en Vrag"},
	{"CIF", "Cost, Insurance and Freight", "Costo, seguro y flete", "Koste, Versekering en Vrag"},
}

var uomSeed = []seedItem{
	{"KGM", "Kilogram", "Kilogramo", "Kilogram"},
	{"GRM", "Gram", "Gramo", "Gram"},
	{"TNE", "Tonne", "Tonelada", "Ton"},
	{"MTR", "Metre", "Metro", "Meter"},
	{"MTK", "Square Metre", "Metro cuadrado", "Vierkante meter"},
	{"MTQ", "Cubic Metre", "Metro cúbico", "Kubieke meter"},
	{"LTR", "Litre", "Litro", "Liter"},
	{"PCE", "Piece", "Pieza", "Stuk"},
	{"HUR", "Hour", "Hora", "Uur"},
	{"DAY", "Day", "Día", "Dag"},
	{"KMH", "Kilometre per Hour", "Kilómetro por hora", "Kilometer per uur"},
	{"CEL", "Degree Celsius", "Grado Celsius", "Graad Celsius"},
}

var hazardClassSeed = []seedItem{
	{"1", "Explosives", "Explosivos", "Plofstowwe"},
	{"2", "Gases", "Gases", "Gasse"},
	{"3", "Flammable Liquids", "Líquidos inflamables", "Ontvlambare Vloeistowwe"},
	{"4", "Flammable Solids", "Sólidos inflamables", "Ontvlambare Vaste Stowwe"},
	{"5", "Oxidizing Substances and Organic Peroxides", "Sustancias comburentes y peróxidos orgánicos", "Oksiderende Stowwe en Organiese Perokside"},
	{"6", "Toxic and Infectious Substances", "Sustancias tóxicas e infecciosas", "Giftige en Aansteeklike Stowwe"},
	{"7", "Radioactive Material", "Material radiactivo", "Radioaktiewe Materiaal"},
	{"8", "Corrosive Substances", "Sustancias corrosivas", "Bytende Stowwe"},
	{"9", "Miscellaneous Dangerous Goods", "Mercancías peligrosas varias", "Diverse Gevaarlike Goedere"},
}

// shipStatusSeed mirrors backend/dictionary/internal/domain/ship.go's
// ShipStatus constants by value — this module has no dependency on the
// shipping backend's code, so the codes are duplicated here as plain
// strings rather than imported.
var shipStatusSeed = []seedItem{
	{"in-transit", "In Transit", "En tránsito", "Onderweg"},
	{"docked", "Docked", "Atracado", "Vasgemeer"},
	{"at-anchor", "At Anchor", "Fondeado", "Voor Anker"},
	{"not-under-command", "Not Under Command", "Sin gobierno", "Nie onder Beheer nie"},
	{"restricted-manoeuvrability", "Restricted Manoeuvrability", "Maniobrabilidad restringida", "Beperkte Wendbaarheid"},
}

// uiCopySeed is the sole authored catalog for Port UI copy (Phase 11.10).
// Codes are vue-i18n message keys, not domain codes. The generated bundled
// English fallback is derived from this seed; do not edit it by hand.
// frontend-port's vue-i18n wiring only consumes en/es (Phase 11.10's scope);
// the af-za column exists in refdata/Postgres like every other seeded
// locale, but isn't surfaced as a selectable UI locale in frontend-port yet.
var uiCopySeed = []seedItem{
	{"filter.all", "All", "Todos", "Alle"},
	{"nav.language", "Language", "Idioma", "Taal"},
	{"nav.fleetManagement", "Fleet Management", "Gestión de flota", "Vlootbestuur"},
	{"nav.portManagement", "Port Management", "Gestión portuaria", "Hawebestuur"},
	{"nav.viewSelector", "View", "Vista", "Aansig"},
	{"select.none", "—", "—", "—"},
	{"app.title", "SeaFreight Flow", "SeaFreight Flow", "SeaFreight Flow"},
	{"app.subtitleFleet", "fleet overview · docked ships · manifests", "visión general de la flota · buques atracados · manifiestos", "vlootoorsig · vasgemeerde skepe · manifeste"},
	{"app.subtitlePort", "terminal yard · ships at port · container operations", "patio de terminal · buques en puerto · operaciones de contenedores", "terminaalwerf · skepe in hawe · houerbedrywighede"},
	{"connection.watching", "watching", "observando", "kyk tans"},
	{"connection.disconnected", "disconnected", "desconectado", "ontkoppel"},
	{"context.fleet", "Fleet", "Flota", "Vloot"},
	{"fallback.unreachable", "UI text: bundled (refdata unreachable)", "Texto de interfaz: incluido (datos de referencia no disponibles)", "Koppelvlakteks: ingebou (verwysingsdata onbereikbaar)"},
	{"fallback.partial", "UI text: partially bundled", "Texto de interfaz: parcialmente incluido", "Koppelvlakteks: gedeeltelik ingebou"},
	{"a11y.lightMode", "Switch to light mode", "Cambiar a modo claro", "Wissel na ligte modus"},
	{"a11y.darkMode", "Switch to dark mode", "Cambiar a modo oscuro", "Wissel na donker modus"},
	{"port.management", "Port Management", "Gestión portuaria", "Hawebestuur"},
	{"port.label", "Port", "Puerto", "Hawe"},
	{"port.select", "select port", "seleccionar puerto", "kies hawe"},
	{"port.add", "Add a shipping port", "Añadir un puerto marítimo", "Voeg 'n skeepvaarthawe by"},
	{"port.addDialog", "Add a shipping port", "Añadir un puerto marítimo", "Voeg 'n skeepvaarthawe by"},
	{"port.namePlaceholder", "port name, e.g. Hamburg", "nombre del puerto, p. ej. Hamburgo", "hawenaam, bv. Hamburg"},
	{"port.addHelp", "Registered immediately in the ports table (Postgres) — usable by every ship arrival and container registration from now on.", "Se registra inmediatamente en la tabla de puertos (Postgres) y queda disponible para cada llegada de buque y registro de contenedor.", "Word onmiddellik in die hawetabel (Postgres) geregistreer — van nou af bruikbaar vir elke skip-aankoms en houerregistrasie."},
	{"action.cancel", "Cancel", "Cancelar", "Kanselleer"},
	{"action.add", "Add", "Añadir", "Voeg by"},
	{"toast.portAddFailed", "Could not add port", "No se pudo añadir el puerto", "Kon nie hawe byvoeg nie"},
	{"fleet.title", "Fleet", "Flota", "Vloot"},
	{"status.label", "Status", "Estado", "Status"},
	{"a11y.registerShip", "Register a new ship", "Registrar un buque nuevo", "Registreer 'n nuwe skip"},
	{"fleet.empty", "No ships match this filter.", "Ningún buque coincide con este filtro.", "Geen skepe pas by hierdie filter nie."},
	{"table.shipId", "Ship ID", "ID de buque", "Skip-ID"},
	{"table.name", "Name", "Nombre", "Naam"},
	{"table.port", "Port", "Puerto", "Hawe"},
	{"table.manifest", "Manifest", "Manifiesto", "Manifes"},
	{"fleet.atSea", "at sea", "en el mar", "op see"},
	{"container.count", "{count} container | {count} containers", "{count} contenedor | {count} contenedores", "{count} houer | {count} houers"},
	{"fleet.register", "Register ship", "Registrar buque", "Registreer skip"},
	{"ship.idPlaceholder", "ship ID, e.g. orient-express", "ID de buque, p. ej. orient-express", "skip-ID, bv. orient-express"},
	{"ship.namePlaceholder", "ship name, e.g. Orient Express", "nombre del buque, p. ej. Orient Express", "skipnaam, bv. Orient Express"},
	{"ship.arrivalPort", "arrival port", "puerto de llegada", "aankomshawe"},
	{"action.register", "Register", "Registrar", "Registreer"},
	{"toast.shipRegistered", "Ship registered", "Buque registrado", "Skip geregistreer"},
	{"shipsAtPort.title", "Ships at Port — {port}", "Buques en puerto — {port}", "Skepe in Hawe — {port}"},
	{"shipsAtPort.selectPort", "Select or add a port to move ships in and out.", "Seleccione o añada un puerto para mover buques dentro y fuera.", "Kies of voeg 'n hawe by om skepe in en uit te beweeg."},
	{"shipsAtPort.atSea", "ship at sea", "buque en el mar", "skip op see"},
	{"action.arrive", "Arrive", "Llegar", "Arriveer"},
	{"shipsAtPort.noShipsAtSea", "No ships at sea — register one from the Fleet panel", "No hay buques en el mar; registre uno desde el panel Flota", "Geen skepe op see nie — registreer een vanaf die Vlootpaneel"},
	{"shipsAtPort.empty", "No ships docked here — send an arrival above.", "No hay buques atracados aquí; registre una llegada arriba.", "Geen skepe hier vasgemeer nie — stuur 'n aankoms hierbo."},
	{"action.depart", "Depart", "Salir", "Vertrek"},
	{"manifest.empty", "No containers on this ship.", "No hay contenedores en este buque.", "Geen houers op hierdie skip nie."},
	{"table.container", "Container", "Contenedor", "Houer"},
	{"table.cargo", "Cargo", "Carga", "Vrag"},
	{"table.origin", "Origin", "Origen", "Oorsprong"},
	{"table.destination", "Destination", "Destino", "Bestemming"},
	{"action.unload", "Unload", "Descargar", "Laai af"},
	{"toast.shipArrived", "Ship arrived", "Buque llegado", "Skip het aangekom"},
	{"toast.shipDeparted", "Ship departed", "Buque salido", "Skip het vertrek"},
	{"toast.departFailed", "Depart failed", "No se pudo salir", "Vertrek het misluk"},
	{"toast.containerUnloaded", "Container unloaded", "Contenedor descargado", "Houer afgelaai"},
	{"terminal.title", "Terminal Yard — {port}", "Patio de terminal — {port}", "Terminaalwerf — {port}"},
	{"terminal.selectPort", "Select or add a port to register and load containers.", "Seleccione o añada un puerto para registrar y cargar contenedores.", "Kies of voeg 'n hawe by om houers te registreer en te laai."},
	{"terminal.registerContainer", "Register container", "Registrar contenedor", "Registreer houer"},
	{"terminal.outbound", "Outbound", "Salida", "Uitgaande"},
	{"terminal.outboundEmpty", "No outbound containers in this yard — register one above.", "No hay contenedores de salida en este patio; registre uno arriba.", "Geen uitgaande houers in hierdie werf nie — registreer een hierbo."},
	{"action.load", "Load", "Cargar", "Laai"},
	{"terminal.noDockedShips", "No ships docked here", "No hay buques atracados aquí", "Geen skepe hier vasgemeer nie"},
	{"terminal.arrived", "Arrived", "Llegados", "Aangekom"},
	{"terminal.arrivedEmpty", "No containers have arrived at their destination here.", "Ningún contenedor ha llegado aquí a su destino.", "Geen houers het hier by hul bestemming aangekom nie."},
	{"container.suffixPlaceholder", "1234567", "1234567", "1234567"},
	{"container.formatHint", "Must be 7 digits, e.g. {containerId}", "Debe tener 7 dígitos, p. ej. {containerId}", "Moet 7 syfers wees, bv. {containerId}"},
	{"container.cargoPlaceholder", "cargo, e.g. Electronics", "carga, p. ej. productos electrónicos", "vrag, bv. Elektronika"},
	{"container.destinationPort", "destination port", "puerto de destino", "bestemmingshawe"},
	{"container.originTerminal", "Origin terminal: {port}", "Terminal de origen: {port}", "Oorsprongterminaal: {port}"},
	{"toast.containerRegistered", "Container registered", "Contenedor registrado", "Houer geregistreer"},
	{"toast.containerLoaded", "Container loaded", "Contenedor cargado", "Houer gelaai"},
	{"toast.loadFailed", "Load failed", "No se pudo cargar", "Laai het misluk"},
}
