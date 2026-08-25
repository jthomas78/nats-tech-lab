<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import OperatingAreaMap from './OperatingAreaMap.vue'
import GitCertificatesTab from './GitCertificatesTab.vue'
import { attrsFor, codeFor, labelFor } from '../itemFields'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Step from 'primevue/step'
import StepList from 'primevue/steplist'
import StepPanel from 'primevue/steppanel'
import StepPanels from 'primevue/steppanels'
import Stepper from 'primevue/stepper'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref, watch } from 'vue'

import {
  activateOrganization,
  registerComplianceDocumentWithFile,
  addFleetAsset,
  approveComplianceDocument,
  getOrganizationAudit,
  getTransporterProfile,
  downloadComplianceDocumentFile,
  listComplianceDocuments,
  listFleetAssets,
  listOperatingAreas,
  addOperatingArea,
  removeOperatingArea,
  listTrackingCredentials,
  configureTrackingCredential,
  listItems,
  listOrganizations,
  reactivateOrganization,
  registerOrganization,
  rejectComplianceDocument,
  suspendOrganization,
  updateOrganization,
} from '../api'
import { useTenantStore } from '../stores/tenant'

// Phase 38d-i — the Transporter surface, split out of OrganizationsPanel.vue.
//
// That panel stays as-is for Shippers. The split is not cosmetic: a Shipper is
// one plain-CRUD Organization aggregate, while a Transporter is that *plus*
// an event-sourced TransporterProfile (ADR-046) carrying vetting state, a
// Temporal saga behind it (38b), fleet assets, and a derived goods-in-transit
// badge. Expressing both through one `partnerType`-branched component would
// mean most of the file was reachable for only one of its two roles.
//
// Two structural decisions, taken 2026-08-20:
//
//   - Detail is a drill-in view, not a table expansion row. Five tabs, an
//     editable form and a vetting stepper do not fit legibly inside a nested
//     table row.
//   - Vetting state is read exclusively from `partner.profile.get` (BR-TP37),
//     which serves the canonical Postgres projection. The browser never talks
//     to Temporal, and nothing here recomputes gitStatus — BR-TP38 derives it
//     server-side, so a badge cannot drift from the documents it describes.

const tenantStore = useTenantStore()
const toast = useToast()

// GOODS_IN_TRANSIT has its own event-sourced tab and registration flow.
// This list belongs to the legacy CRUD Documents tab only.
const DOCUMENT_TYPES = [
  'CIPC',
  'DIRECTOR_ID',
  'BANK_CONFIRMATION_LETTER',
  'TERMS_AND_CONDITIONS',
]

// ── List ──────────────────────────────────────────────────────────────────

const partners = ref([])
const loading = ref(false)
const error = ref('')
// Keyed by partner id — { hasProfile, profile, gitStatus } from BR-TP37.
const profiles = ref({})
// Operating areas per row for the list view, keyed by organization id. A
// separate ref from `operatingAreas` (the drill-in's editable copy) because the
// list holds every partner while the drill-in holds one, so every write to the
// drill-in must also write through here — see `toggleArea`.
const areasByPartner = ref({})

async function load() {
  if (!tenantStore.context) return
  loading.value = true
  error.value = ''
  try {
    const res = await listOrganizations(tenantStore.context)
    partners.value = (res.organizations ?? []).filter((tp) => tp.type === 'TRANSPORTER')
    // One profile.get and one operating-area.list per row, in parallel. This
    // is deliberately N+1: the list endpoint returns Organization rows only
    // (the two aggregates are separate by ADR-046, and a list-level join
    // would put vetting state on the wrong side of that boundary), and a
    // context holds few partners in this POC. A per-row failure must not
    // blank the whole table, so each result is settled independently and a
    // rejection just leaves that row's vetting or areas cell empty.
    const [profileResults, areaResults] = await Promise.all([
      Promise.allSettled(
        partners.value.map((tp) => getTransporterProfile(tenantStore.context, tp.id)),
      ),
      Promise.allSettled(
        partners.value.map((tp) => listOperatingAreas(tenantStore.context, tp.id)),
      ),
    ])
    const next = {}
    profileResults.forEach((r, i) => {
      if (r.status === 'fulfilled') next[partners.value[i].id] = r.value
    })
    profiles.value = next
    const nextAreas = {}
    areaResults.forEach((r, i) => {
      if (r.status === 'fulfilled') nextAreas[partners.value[i].id] = r.value.operatingAreas ?? []
    })
    areasByPartner.value = nextAreas
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(() => tenantStore.context, load, { immediate: true })

// A context switch must not leave the operator inside a detail view belonging
// to the previous context.
watch(() => tenantStore.context, () => { selected.value = null })

// ── Drill-in navigation ───────────────────────────────────────────────────

const selected = ref(null)
const activeTab = ref('company')

const documents = ref([])
const fleetAssets = ref([])
const auditEvents = ref([])
const operatingAreas = ref([])
const trackingCredentials = ref([])
const detailLoading = ref(false)

const selectedProfile = computed(() => (selected.value ? profiles.value[selected.value.id] : null))
const gitStatus = computed(() => selectedProfile.value?.gitStatus ?? '')

async function openDetail(tp) {
  selected.value = tp
  activeTab.value = 'company'
  seedCompanyForm(tp)
  // The region corpus is fetched alongside the detail rather than when the
  // Operating Areas tab is first opened: it is small, cached after the first
  // call, and loading it lazily made the tab render its "no corpus" empty
  // state for a beat before filling in, which reads as a real error.
  await Promise.all([refreshDetail(), loadRegionCorpus()])
}

function closeDetail() {
  selected.value = null
  documents.value = []
  fleetAssets.value = []
  auditEvents.value = []
}

async function refreshDetail() {
  const tp = selected.value
  if (!tp) return
  detailLoading.value = true
  try {
    const [docs, fleet, audit, profile, areas, creds] = await Promise.all([
      listComplianceDocuments(tenantStore.context, tp.id),
      listFleetAssets(tenantStore.context, tp.id),
      getOrganizationAudit(tenantStore.context, tp.id),
      getTransporterProfile(tenantStore.context, tp.id),
      listOperatingAreas(tenantStore.context, tp.id),
      listTrackingCredentials(tenantStore.context, tp.id),
    ])
    documents.value = (docs.documents ?? []).filter((doc) => doc.type !== 'GOODS_IN_TRANSIT')
    fleetAssets.value = fleet.fleetAssets ?? []
    auditEvents.value = audit.events ?? []
    operatingAreas.value = areas.operatingAreas ?? []
    trackingCredentials.value = creds.trackingCredentials ?? []
    profiles.value = { ...profiles.value, [tp.id]: profile }
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to load details', detail: e.message, life: 5000 })
  } finally {
    detailLoading.value = false
  }
}

// ── Operating Areas (BR-TP46-BR-TP50) ─────────────────────────────────────
//
// The map and the checklist are two views of one selection, deliberately.
// A polygon is quick for "the whole Western Cape" but imprecise and
// unusable without a pointer; the list is exact, keyboard-reachable and
// readable by a screen reader. Neither is a fallback for the other — both
// write through the same handler, so they cannot drift.

const regionCorpus = ref([])       // { code, name, country } from refdata
const areaBusy = ref(false)

// Regions live in the _platform context: they are `standards` corpora
// (BR-D46), and refdata's item lookup is an exact context match with no
// ancestor walk, so asking in the tenant's own business-unit context
// returns nothing.
const PLATFORM_CONTEXT = '_platform'

async function loadRegionCorpus() {
  if (regionCorpus.value.length) return
  try {
    // An explicit locale matters: with locale '' the service still resolves
    // one, and when nothing matches it falls through to BR-D03's terminal
    // code-echo — so every row came back labelled with its own code.
    const res = await listItems(PLATFORM_CONTEXT, 'region', { locale: 'en' })
    // codeFor/labelFor/attrsFor rather than reading the fields directly:
    // type.list returns ItemGetResponse per row ({ item, label, ... }),
    // while other paths return the bare item, and this app already has one
    // helper that absorbs both shapes. Reading `item.code` here worked in
    // neither — it silently yielded an empty corpus.
    regionCorpus.value = (res?.items ?? []).map((entry) => {
      const code = codeFor(entry)
      // Prefer the localized label — that is BR-D48's whole point, since
      // `Wes-Kaap` is a label on the canonical item rather than a separate
      // region. But a label equal to the code IS the code-echo fallback,
      // not a real translation, so treat it as absent and use the stored
      // name instead of showing the operator "BW-CE  BW-CE".
      const label = labelFor(entry)
      return {
        code,
        name: label && label !== code ? label : (attrsFor(entry)?.name || code),
        country: String(code).split('-')[0],
      }
    })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to load regions', detail: e.message, life: 5000 })
  }
}

// areaCodes orders a row's grants countries-first, then alphabetically inside
// each group, so the list reads the same way every time regardless of the
// order the grants were added in.
function areaCodes(partnerId) {
  const areas = areasByPartner.value[partnerId] ?? []
  const countries = areas.filter((a) => a.level === 'COUNTRY').map((a) => a.code).sort()
  const regions = areas.filter((a) => a.level !== 'COUNTRY').map((a) => a.code).sort()
  return [...countries, ...regions]
}

// assignedCodes is what both views render from, so a click on the map and a
// tick in the list cannot disagree about what is selected.
const assignedCodes = computed(() => new Set(operatingAreas.value.map((a) => a.code)))

// countriesCovered are the country-level assignments. A region inside one
// is not merely redundant — BR-TP48 rejects it — so the checklist disables
// those rows and says why, rather than letting the operator click into a
// server-side error.
const countriesCovered = computed(
  () => new Set(operatingAreas.value.filter((a) => a.level === 'COUNTRY').map((a) => a.code)),
)

const regionsByCountry = computed(() => {
  const groups = new Map()
  for (const region of regionCorpus.value) {
    if (!groups.has(region.country)) groups.set(region.country, [])
    groups.get(region.country).push(region)
  }
  return [...groups.entries()].map(([country, regions]) => ({ country, regions }))
})

function areaDisabledReason(level, code, country) {
  if (level === 'REGION' && countriesCovered.value.has(country)) {
    return `Covered by the ${country} country-level assignment`
  }
  if (level === 'COUNTRY') {
    const inner = operatingAreas.value.filter((a) => a.level === 'REGION' && a.countryCode === code)
    if (inner.length) return `${inner.length} region(s) of ${code} are assigned individually`
  }
  return ''
}

async function toggleArea(level, code, country) {
  if (areaBusy.value) return
  const reason = areaDisabledReason(level, code, country)
  if (reason && !assignedCodes.value.has(code)) {
    // Surfaced rather than swallowed: BR-TP48 rejects rather than silently
    // collapsing, so the operator has to resolve the ambiguity and should
    // be told what it is.
    toast.add({ severity: 'warn', summary: 'Overlapping coverage', detail: reason, life: 5000 })
    return
  }
  areaBusy.value = true
  try {
    if (assignedCodes.value.has(code)) {
      await removeOperatingArea(tenantStore.context, selected.value.id, level, code)
    } else {
      await addOperatingArea(tenantStore.context, selected.value.id, level, code)
    }
    const res = await listOperatingAreas(tenantStore.context, selected.value.id)
    operatingAreas.value = res.operatingAreas ?? []
    // Write through to the list's own copy from the same response, so the
    // Operating Areas column is already correct when the operator navigates
    // back instead of showing pre-edit codes until the next full load().
    areasByPartner.value = {
      ...areasByPartner.value,
      [selected.value.id]: operatingAreas.value,
    }
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Coverage change failed', detail: e.message, life: 6000 })
  } finally {
    areaBusy.value = false
  }
}

// ── Tracking Credentials (BR-TP51-BR-TP55) ────────────────────────────────

const PROVIDERS = ['CARTRACK', 'MIX_TELEMATICS', 'WEBFLEET', 'CTRACK', 'NETSTAR']
const CREDENTIAL_TYPES = ['API_KEY', 'USERNAME_PASSWORD', 'METADATA_ONLY']

const credentialForm = ref({ provider: '', credentialType: '', payload: '' })
const credentialBusy = ref(false)

const credentialValid = computed(
  () =>
    credentialForm.value.provider &&
    credentialForm.value.credentialType &&
    // METADATA_ONLY genuinely has no secret to enter, so requiring one
    // would make a legitimate V2 case unrepresentable.
    (credentialForm.value.credentialType === 'METADATA_ONLY' || credentialForm.value.payload.length > 0),
)

async function saveCredential() {
  if (!credentialValid.value || credentialBusy.value) return
  credentialBusy.value = true
  try {
    await configureTrackingCredential(tenantStore.context, selected.value.id, {
      provider: credentialForm.value.provider,
      credentialType: credentialForm.value.credentialType,
      payload: credentialForm.value.payload,
    })
    // Clear the payload immediately. BR-TP52 means the value can never be
    // read back, so leaving it in the field would suggest the app is
    // holding it — and a stale secret sitting in a form is a secret waiting
    // to be shoulder-surfed or submitted to the wrong provider.
    credentialForm.value = { provider: '', credentialType: '', payload: '' }
    const res = await listTrackingCredentials(tenantStore.context, selected.value.id)
    trackingCredentials.value = res.trackingCredentials ?? []
    toast.add({ severity: 'success', summary: 'Credential stored', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Could not store credential', detail: e.message, life: 6000 })
  } finally {
    credentialBusy.value = false
  }
}

// reselect re-reads the partner row itself (name/status/version) from the
// list. There is no single-partner get endpoint, and the list is small.
async function reselect() {
  const res = await listOrganizations(tenantStore.context)
  const fresh = (res.organizations ?? []).find((p) => p.id === selected.value?.id)
  if (fresh) selected.value = fresh
  partners.value = (res.organizations ?? []).filter((tp) => tp.type === 'TRANSPORTER')
  return fresh
}

// ── Company Information (BR-TP32/BR-TP34/BR-TP39) ─────────────────────────

const companyForm = reactive({
  name: '',
  tradingAs: '',
  companyName: '',
  registrationNo: '',
  vatRegistrationNo: '',
})
const companySaving = ref(false)
const companyError = ref('')
// BR-TP39 — set only on a 409. Holds nothing but the flag: the operator's
// typed values stay in companyForm untouched, which is the whole point.
const companyConflict = ref(false)

function seedCompanyForm(tp) {
  companyForm.name = tp.name ?? ''
  companyForm.tradingAs = tp.tradingAs ?? ''
  companyForm.companyName = tp.companyName ?? ''
  companyForm.registrationNo = tp.registrationNo ?? ''
  companyForm.vatRegistrationNo = tp.vatRegistrationNo ?? ''
  companyError.value = ''
  companyConflict.value = false
}

async function saveCompany(version) {
  if (!companyForm.name) return
  companySaving.value = true
  companyError.value = ''
  try {
    await updateOrganization(tenantStore.context, selected.value.id, version, { ...companyForm })
    companyConflict.value = false
    const fresh = await reselect()
    if (fresh) seedCompanyForm(fresh)
    toast.add({ severity: 'success', summary: 'Company information saved', life: 3000 })
  } catch (e) {
    // BR-TP39: a version conflict is not a generic failure and must not be
    // reported as one. The `conflict` flag comes from the service's own error
    // envelope (shared/browserrpc ErrorResponse.Conflict), so this does not
    // depend on the wording of the message.
    if (e.conflict) {
      companyConflict.value = true
      companyError.value = ''
    } else {
      companyError.value = e.message
    }
  } finally {
    companySaving.value = false
  }
}

// Reload — discard my edits and take theirs. The destructive choice, so it is
// never the automatic one.
async function reloadCompany() {
  const fresh = await reselect()
  if (fresh) seedCompanyForm(fresh)
}

// Overwrite — keep my edits and win. Re-reads the row purely to learn the
// current version, then resubmits the operator's values against it. This is a
// deliberate last-write-wins escape hatch, not a merge: BR-TP34 guarantees the
// write is rejected if someone else moves again in between, so the worst case
// is another conflict banner rather than a silent lost update.
async function overwriteCompany() {
  const fresh = await reselect()
  if (!fresh) {
    companyError.value = 'This transporter no longer exists.'
    companyConflict.value = false
    return
  }
  await saveCompany(fresh.version)
}

// ── Lifecycle (BR-TP03-BR-TP05) ───────────────────────────────────────────

async function activate(tp) {
  try {
    await activateOrganization(tenantStore.context, tp.id)
    await reselect()
    await refreshDetail()
    toast.add({ severity: 'success', summary: 'Activated', detail: tp.name, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to activate', detail: e.message, life: 6000 })
  }
}

const suspendOpen = ref(false)
const suspendReason = ref('')
const suspendSaving = ref(false)
const suspendError = ref('')

function openSuspend() {
  suspendReason.value = ''
  suspendError.value = ''
  suspendOpen.value = true
}

async function submitSuspend() {
  if (!suspendReason.value) return
  suspendSaving.value = true
  suspendError.value = ''
  try {
    await suspendOrganization(tenantStore.context, selected.value.id, suspendReason.value)
    suspendOpen.value = false
    await reselect()
    await refreshDetail()
    toast.add({ severity: 'success', summary: 'Suspended', life: 3000 })
  } catch (e) {
    suspendError.value = e.message
  } finally {
    suspendSaving.value = false
  }
}

async function reactivate(tp) {
  try {
    await reactivateOrganization(tenantStore.context, tp.id)
    await reselect()
    await refreshDetail()
    toast.add({ severity: 'success', summary: 'Reactivated', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to reactivate', detail: e.message, life: 6000 })
  }
}

// ── Registration wizard (BR-TP35) ─────────────────────────────────────────

const wizardOpen = ref(false)
const wizardStep = ref('1')
const wizardSaving = ref(false)
const wizardError = ref('')
// The partner created by step 1. Fleet assets and documents can only be added
// against an existing id, so registration genuinely commits at step 1 rather
// than at Finish — the wizard's own copy is shown alongside the steps so that
// is visible to the operator instead of being a surprise on cancel.
const wizardPartner = ref(null)
const wizardForm = reactive({
  name: '',
  tradingAs: '',
  companyName: '',
  registrationNo: '',
  vatRegistrationNo: '',
})
const wizardFleet = reactive({ registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })
const wizardFleetAdded = ref([])
// Phase 40: a compliance document is a dropped file, so the wizard's document
// step carries the file rather than a typed reference.
const wizardDoc = reactive({ type: DOCUMENT_TYPES[0], file: null })
const wizardDocFileInput = ref(null)
const wizardDocsAdded = ref([])

function openWizard() {
  wizardStep.value = '1'
  wizardPartner.value = null
  wizardError.value = ''
  Object.assign(wizardForm, { name: '', tradingAs: '', companyName: '', registrationNo: '', vatRegistrationNo: '' })
  Object.assign(wizardFleet, { registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })
  Object.assign(wizardDoc, { type: DOCUMENT_TYPES[0], file: null })
  wizardFleetAdded.value = []
  wizardDocsAdded.value = []
  wizardOpen.value = true
}

async function wizardRegister(activateCallback) {
  if (!wizardForm.name) return
  wizardSaving.value = true
  wizardError.value = ''
  try {
    // BR-TP35 — Register accepts the Company Information fields directly, so
    // the wizard's first step is one write, not a register-then-update pair
    // that could half-fail.
    wizardPartner.value = await registerOrganization(tenantStore.context, {
      ...wizardForm,
      type: 'TRANSPORTER',
    })
    await load()
    activateCallback('2')
  } catch (e) {
    wizardError.value = e.message
  } finally {
    wizardSaving.value = false
  }
}

async function wizardAddFleet() {
  if (!wizardFleet.registrationNo || !wizardFleet.vehicleTypeCode) return
  wizardSaving.value = true
  wizardError.value = ''
  try {
    await addFleetAsset(tenantStore.context, wizardPartner.value.id, tenantStore.tenant, { ...wizardFleet })
    wizardFleetAdded.value = [...wizardFleetAdded.value, wizardFleet.registrationNo]
    Object.assign(wizardFleet, { registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })
  } catch (e) {
    wizardError.value = e.message
  } finally {
    wizardSaving.value = false
  }
}

function chooseWizardDocFile() {
  if (!wizardDocFileInput.value) return
  wizardDocFileInput.value.value = ''
  wizardDocFileInput.value.click()
}

function acceptWizardDocFile(file) {
  wizardError.value = ''
  const refusal = fileRefusal(file)
  if (refusal) {
    wizardError.value = refusal
    return
  }
  wizardDoc.file = file
}

async function wizardAddDoc() {
  if (!wizardDoc.type || !wizardDoc.file) return
  wizardSaving.value = true
  wizardError.value = ''
  try {
    await registerComplianceDocumentWithFile(
      tenantStore.context, wizardPartner.value.id, wizardDoc.type, wizardDoc.file,
    )
    wizardDocsAdded.value = [...wizardDocsAdded.value, wizardDoc.type]
    wizardDoc.file = null
  } catch (e) {
    wizardError.value = e.message
  } finally {
    wizardSaving.value = false
  }
}

async function finishWizard() {
  const created = wizardPartner.value
  wizardOpen.value = false
  await load()
  if (created) {
    const tp = partners.value.find((p) => p.id === created.id)
    if (tp) await openDetail(tp)
  }
}

// ── Documents (BR-TP07-BR-TP11, BR-TP29-BR-TP31) ──────────────────────────

const addDocOpen = ref(false)
const addDocSaving = ref(false)
const addDocError = ref('')
const addDocForm = reactive({ type: DOCUMENT_TYPES[0], file: null })

function openAddDocument() {
  addDocForm.type = DOCUMENT_TYPES[0]
  addDocForm.file = null
  addDocError.value = ''
  addDocOpen.value = true
}

function chooseAddDocFile() {
  if (!fileInput.value) return
  fileInput.value.value = ''
  fileInput.value.click()
}

function acceptAddDocFile(file) {
  addDocError.value = ''
  const refusal = fileRefusal(file)
  if (refusal) {
    addDocError.value = refusal
    return
  }
  // BR-TP74's pre-check against what this organization already holds. The
  // service refuses a duplicate anyway; this saves a doomed round trip.
  if ((documents.value ?? []).some((doc) => (doc.documentName || '') === file.name)) {
    addDocError.value = `${file.name} has already been registered for this organization.`
    return
  }
  addDocForm.file = file
}

async function submitAddDocument() {
  if (!addDocForm.type || !addDocForm.file) return
  addDocSaving.value = true
  addDocError.value = ''
  try {
    await registerComplianceDocumentWithFile(
      tenantStore.context, selected.value.id, addDocForm.type, addDocForm.file,
    )
    addDocOpen.value = false
    await refreshDetail()
    toast.add({ severity: 'success', summary: 'Document registered', detail: addDocForm.type, life: 3000 })
  } catch (e) {
    addDocError.value = e.message
  } finally {
    addDocSaving.value = false
  }
}

// ── Document files (BR-TP40-BR-TP45, Phase 38c-ii) ────────────────────────
//
// The transfer is two calls, not one: a ticket minted over the authenticated
// NATS connection, then the bytes over HTTP carrying only that ticket
// (BR-TP41). api.js owns both halves; this component owns the interaction.
//
// Phase 40 removed the per-row Upload control. Registration is the only way
// bytes arrive now, so no row can exist without a file and there is nothing
// left for that button to do.

const MAX_FILE_BYTES = 10 * 1024 * 1024
const maxFileMb = MAX_FILE_BYTES / (1024 * 1024)

const fileInput = ref(null)
const busyDocId = ref('')
const fileError = ref('')

// A courtesy check, not a control: the service enforces BR-TP44 regardless,
// and does so on the bytes it actually reads. Checking here only spares the
// operator a doomed upload and a spent ticket.
function fileRefusal(file) {
  if (!file) return 'No file chosen.'
  if (file.size > MAX_FILE_BYTES) {
    return `${file.name} is ${formatBytes(file.size)} — the limit is ${maxFileMb} MB.`
  }
  if (file.size === 0) return `${file.name} is empty.`
  return ''
}

async function downloadFile(doc) {
  busyDocId.value = doc.id
  fileError.value = ''
  try {
    const blob = await downloadComplianceDocumentFile(tenantStore.context, selected.value.id, doc.id)
    // An object URL plus a synthetic click is the only way to name the saved
    // file from a fetched blob — the response's own Content-Disposition
    // doesn't apply to bytes read by script.
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = doc.documentName
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    fileError.value = e.status === 403
      ? 'The download authorization expired. Try again.'
      : e.message
  } finally {
    busyDocId.value = ''
  }
}

function formatBytes(bytes) {
  if (!bytes) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

async function reviewDoc(action, doc) {
  const fn = { approve: approveComplianceDocument, reject: rejectComplianceDocument }[action]
  try {
    await fn(tenantStore.context, selected.value.id, doc.id)
    await refreshDetail()
  } catch (e) {
    toast.add({ severity: 'error', summary: `Failed to ${action}`, detail: e.message, life: 5000 })
  }
}

// ── Fleet assets (BR-TP12-BR-TP14) ────────────────────────────────────────

const addFleetOpen = ref(false)
const addFleetSaving = ref(false)
const addFleetError = ref('')
const addFleetForm = reactive({ registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })

function openAddFleetAsset() {
  Object.assign(addFleetForm, { registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })
  addFleetError.value = ''
  addFleetOpen.value = true
}

async function submitAddFleetAsset() {
  if (!addFleetForm.registrationNo || !addFleetForm.vehicleTypeCode) return
  addFleetSaving.value = true
  addFleetError.value = ''
  try {
    await addFleetAsset(tenantStore.context, selected.value.id, tenantStore.tenant, { ...addFleetForm })
    addFleetOpen.value = false
    await refreshDetail()
    toast.add({ severity: 'success', summary: 'Fleet asset added', life: 3000 })
  } catch (e) {
    addFleetError.value = e.message
  } finally {
    addFleetSaving.value = false
  }
}

// ── Presentation helpers ──────────────────────────────────────────────────

function statusSeverity(status) {
  if (status === 'active') return 'success'
  if (status === 'suspended') return 'danger'
  return 'secondary' // registered
}

function docStatusSeverity(status) {
  if (status === 'APPROVED') return 'success'
  if (status === 'REJECTED') return 'danger'
  if (status === 'SUPERSEDED') return 'contrast'
  return 'secondary' // FOR_REVIEW / SUPERSEDED
}

// BR-TP38's five values. Expired and Rejected are both "no cover today", so
// both read as danger and are distinguished by their label.
function gitSeverity(status) {
  if (status === 'Active') return 'success'
  if (status === 'Pending') return 'warn'
  if (status === 'Expired' || status === 'Rejected') return 'danger'
  return 'secondary' // None
}

function vettingSeverity(status) {
  if (status === 'Vetted') return 'success'
  // BR-TP63: CoverLapsed reads as danger, not warn. The transporter was
  // vetted and no longer is — its organization has been suspended and its
  // fleet is unassignable, which is the same severity as a rejection even
  // though it arrived by a different route.
  if (status === 'Rejected' || status === 'CoverLapsed') return 'danger'
  if (status === 'InReview') return 'warn'
  return 'secondary' // Awaiting
}

// The vetting stepper's linear spine. Neither terminal failure is a step of
// its own: Rejected is the review step failing and CoverLapsed (BR-TP63) is
// the Vetted step failing, each shown in place rather than as a fourth
// position — so the stepper never implies a failure comes *after* vetting.
const VETTING_STEPS = [
  { key: 'Awaiting', label: 'Awaiting' },
  { key: 'InReview', label: 'In Review' },
  { key: 'Vetted', label: 'Vetted' },
]

// Where each terminal failure lands on the spine, and what that step is called
// once it has failed.
const VETTING_FAILURES = {
  Rejected: { index: 1, label: 'Rejected' },
  CoverLapsed: { index: 2, label: 'Cover Lapsed' },
}

const vettingFailure = computed(() => VETTING_FAILURES[selectedProfile.value?.profile?.status] ?? null)

const vettingIndex = computed(() => {
  const status = selectedProfile.value?.profile?.status
  if (vettingFailure.value) return vettingFailure.value.index
  const i = VETTING_STEPS.findIndex((s) => s.key === status)
  return i < 0 ? 0 : i
})

const isRejected = computed(() => selectedProfile.value?.profile?.status === 'Rejected')
const isLapsed = computed(() => selectedProfile.value?.profile?.status === 'CoverLapsed')

function formatDate(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}
</script>

<template>
  <div class="lab-panel transporter-panel">
    <!-- ── List view ────────────────────────────────────────────────────── -->
    <template v-if="!selected">
      <div class="panel-header">
        <span class="panel-title">Transporters</span>
        <div class="header-actions">
          <Button
            icon="pi pi-refresh"
            text
            rounded
            size="small"
            :loading="loading"
            aria-label="Refresh"
            @click="load"
          />
          <Button
            label="Register Transporter"
            icon="pi pi-plus"
            size="small"
            :disabled="!tenantStore.context"
            @click="openWizard"
          />
        </div>
      </div>

      <p class="lab-muted description">
        Transporter registration and vetting (Phase 38). Each row is a <code>Organization</code> plus its
        event-sourced <code>TransporterProfile</code> — vetting state and the derived goods-in-transit badge come
        from the service's canonical projection (BR-TP37/BR-TP38), never from the browser.
      </p>

      <p
        v-if="!tenantStore.context"
        class="lab-muted"
      >
        Select a tenant and fleet context above to manage transporters.
      </p>
      <p
        v-if="error"
        class="error-text"
      >
        {{ error }}
      </p>

      <DataTable
        :value="partners"
        size="small"
        paginator
        :rows="10"
        row-hover
        class="partners-table"
        @row-click="openDetail($event.data)"
      >
        <template #empty>
          <span class="lab-muted">No transporters registered in this context yet.</span>
        </template>
        <Column
          header="Company Name"
          field="name"
        />
        <!-- Operating areas as codes, countries first: a COUNTRY grant
             subsumes the regions inside it (BR-TP48), so showing it first is
             what tells the operator the rest of that country's regions are
             already covered. Codes rather than names because a name column
             wide enough for "KwaZulu-Natal" crowds out the status badges the
             list exists for.

             Every grant is shown, with no truncation and no "+N" more count:
             coverage is what an operator scans this column to compare, and a
             hidden tail makes two rows look alike when they aren't. The codes
             fill the column's width and wrap, growing the row instead. -->
        <Column
          header="Operating Areas"
          style="min-width: 12rem; max-width: 20rem"
        >
          <template #body="{ data }">
            <div
              v-if="areaCodes(data.id).length"
              class="area-codes"
            >
              <Tag
                v-for="code in areaCodes(data.id)"
                :key="code"
                severity="secondary"
                :value="code"
              />
            </div>
            <span
              v-else
              class="lab-muted"
            >—</span>
          </template>
        </Column>
        <Column header="Status">
          <template #body="{ data }">
            <Tag
              :severity="statusSeverity(data.status)"
              :value="data.status"
            />
          </template>
        </Column>
        <Column header="Vetting">
          <template #body="{ data }">
            <Tag
              v-if="profiles[data.id]?.hasProfile"
              :severity="vettingSeverity(profiles[data.id].profile.status)"
              :value="profiles[data.id].profile.status"
            />
            <span
              v-else
              class="lab-muted"
            >—</span>
          </template>
        </Column>
        <Column header="GIT">
          <template #body="{ data }">
            <Tag
              v-if="profiles[data.id]"
              :severity="gitSeverity(profiles[data.id].gitStatus)"
              :value="profiles[data.id].gitStatus"
            />
            <span
              v-else
              class="lab-muted"
            >—</span>
          </template>
        </Column>
        <Column
          header=""
          style="width: 3rem"
        >
          <template #body>
            <i class="pi pi-angle-right lab-muted" />
          </template>
        </Column>
      </DataTable>
    </template>

    <!-- ── Detail view ──────────────────────────────────────────────────── -->
    <template v-else>
      <div class="crumb">
        <Button
          label="Transporters"
          icon="pi pi-angle-left"
          text
          size="small"
          @click="closeDetail"
        />
        <span class="lab-muted">/</span>
        <span class="crumb-current">{{ selected.name }}</span>
      </div>

      <div class="detail-header">
        <div class="detail-ident">
          <span class="detail-name">{{ selected.name }}</span>
          <div class="detail-badges">
            <Tag
              :severity="statusSeverity(selected.status)"
              :value="selected.status"
            />
            <Tag
              v-if="selectedProfile?.hasProfile"
              :severity="vettingSeverity(selectedProfile.profile.status)"
              :value="selectedProfile.profile.status"
            />
            <Tag
              v-if="gitStatus"
              :severity="gitSeverity(gitStatus)"
              :value="`GIT: ${gitStatus}`"
            />
          </div>
        </div>
        <div class="header-actions">
          <Button
            icon="pi pi-refresh"
            text
            rounded
            size="small"
            :loading="detailLoading"
            aria-label="Refresh"
            @click="refreshDetail"
          />
          <Button
            v-if="selected.status === 'registered'"
            label="Activate"
            icon="pi pi-play"
            size="small"
            @click="activate(selected)"
          />
          <Button
            v-if="selected.status === 'active'"
            label="Suspend"
            icon="pi pi-ban"
            size="small"
            severity="danger"
            outlined
            @click="openSuspend"
          />
          <Button
            v-if="selected.status === 'suspended'"
            label="Reactivate"
            icon="pi pi-play"
            size="small"
            @click="reactivate(selected)"
          />
        </div>
      </div>

      <Tabs v-model:value="activeTab">
        <TabList>
          <Tab value="company">
            Company Information
          </Tab>
          <Tab value="fleet">
            Fleet
          </Tab>
          <Tab value="documents">
            Documents
          </Tab>
          <Tab value="git-certificates">
            GIT Certificates
          </Tab>
          <Tab value="vetting">
            Vetting
          </Tab>
          <Tab value="areas">
            Operating Areas
          </Tab>
          <Tab value="tracking">
            Tracking
          </Tab>
          <Tab value="rates">
            Rate Sheets
          </Tab>
        </TabList>
        <TabPanels>
          <!-- Company Information (BR-TP32/BR-TP34/BR-TP39) -->
          <TabPanel value="company">
            <div
              v-if="companyConflict"
              class="conflict-banner"
            >
              <div class="conflict-head">
                <i class="pi pi-exclamation-triangle" />
                <span><strong>Company Information</strong> was changed by someone else while you were editing.</span>
              </div>
              <p class="conflict-body">
                Your edits below have been kept exactly as you typed them — nothing has been saved and nothing has
                been discarded. Choose which version wins.
              </p>
              <div class="conflict-actions">
                <Button
                  label="Reload theirs"
                  icon="pi pi-download"
                  size="small"
                  outlined
                  @click="reloadCompany"
                />
                <Button
                  label="Overwrite with mine"
                  icon="pi pi-upload"
                  size="small"
                  severity="danger"
                  :loading="companySaving"
                  @click="overwriteCompany"
                />
              </div>
            </div>

            <div class="form-grid">
              <div class="form-field">
                <label for="co-name">Name</label>
                <InputText
                  id="co-name"
                  v-model="companyForm.name"
                />
              </div>
              <div class="form-field">
                <label for="co-trading-as">Trading As</label>
                <InputText
                  id="co-trading-as"
                  v-model="companyForm.tradingAs"
                />
              </div>
              <div class="form-field">
                <label for="co-company">Company Name</label>
                <InputText
                  id="co-company"
                  v-model="companyForm.companyName"
                />
              </div>
              <div class="form-field">
                <label for="co-reg">Registration No</label>
                <InputText
                  id="co-reg"
                  v-model="companyForm.registrationNo"
                />
              </div>
              <div class="form-field">
                <label for="co-vat">VAT Registration No</label>
                <InputText
                  id="co-vat"
                  v-model="companyForm.vatRegistrationNo"
                />
              </div>
            </div>

            <p class="lab-muted hint">
              <code>Type</code> and <code>Context</code> are immutable (BR-TP32) and lifecycle status changes through
              its own actions, so none of the three is editable here. Saving sends the version this form was loaded
              at (<code>v{{ selected.version }}</code>); a concurrent change is rejected rather than overwritten
              (BR-TP34).
            </p>

            <p
              v-if="companyError"
              class="error-text"
            >
              {{ companyError }}
            </p>

            <div class="tab-actions">
              <Button
                label="Revert"
                text
                size="small"
                :disabled="companySaving"
                @click="seedCompanyForm(selected)"
              />
              <Button
                label="Save"
                size="small"
                :loading="companySaving"
                :disabled="!companyForm.name"
                @click="saveCompany(selected.version)"
              />
            </div>
          </TabPanel>

          <!-- Fleet (BR-TP12-BR-TP14) -->
          <TabPanel value="fleet">
            <div class="tab-head">
              <span class="lab-muted">Vehicle types are validated against refdata-service's <code>vehicle-type</code> corpus (BR-TP14).</span>
              <Button
                label="Add Fleet Asset"
                icon="pi pi-plus"
                size="small"
                @click="openAddFleetAsset"
              />
            </div>
            <DataTable
              :value="fleetAssets"
              size="small"
              class="sub-table"
            >
              <template #empty>
                <span class="lab-muted">No fleet assets registered yet.</span>
              </template>
              <Column
                header="Registration No"
                field="registrationNo"
              />
              <Column
                header="Make"
                field="make"
              />
              <Column
                header="Model"
                field="model"
              />
              <Column
                header="VIN"
                field="vin"
                class="mono-col"
              />
              <Column
                header="Vehicle Type"
                field="vehicleTypeCode"
              />
            </DataTable>
          </TabPanel>

          <!-- Documents (BR-TP29-BR-TP31; upload deferred to 38c-ii) -->
          <TabPanel value="documents">
            <div class="tab-head">
              <span class="lab-muted">Superseded documents are retained server-side but not listed — this is the current set (BR-TP31).</span>
              <Button
                label="Add Document"
                icon="pi pi-plus"
                size="small"
                @click="openAddDocument"
              />
            </div>

            <div class="deferred-note">
              <i class="pi pi-info-circle" />
              <span>
                Files live in a <strong>NATS Object Store</strong> bucket (38c-ii). A document's bytes are
                <strong>write-once</strong>: to correct a file, add a new document of the same type — that supersedes
                this one and both remain retrievable (BR-TP43). Maximum {{ maxFileMb }} MB per file.
              </span>
            </div>

            <div
              v-if="fileError"
              class="file-error"
            >
              <i class="pi pi-exclamation-triangle" />
              <span>{{ fileError }}</span>
            </div>

            <!-- The Add Document dialog's picker. One hidden input serves it
                 because registration is now the only path bytes take. -->
            <input
              ref="fileInput"
              type="file"
              accept="application/pdf,image/*"
              class="hidden-file-input"
              @change="acceptAddDocFile($event.target.files?.[0])"
            >

            <DataTable
              :value="documents"
              size="small"
              class="sub-table"
            >
              <template #empty>
                <span class="lab-muted">No documents registered yet.</span>
              </template>
              <Column
                header="Type"
                field="type"
              />
              <Column header="Status">
                <template #body="{ data: doc }">
                  <Tag
                    :severity="docStatusSeverity(doc.status)"
                    :value="doc.status"
                  />
                </template>
              </Column>
              <Column
                header="Document Name"
                field="documentName"
                class="mono-col"
              />
              <Column
                header="Document ID"
                field="id"
                class="mono-col"
              />
              <Column header="Expires">
                <template #body="{ data: doc }">
                  {{ doc.expiresAt ? formatDate(doc.expiresAt * 1000) : '—' }}
                </template>
              </Column>
              <Column
                header="File"
                style="width: 15rem"
              >
                <template #body="{ data: doc }">
                  <!-- Every document has bytes now (Phase 40), and they are
                       write-once (BR-TP43) — so this cell only ever downloads.
                       Replacing a file means registering another document. -->
                  <div class="file-cell">
                    <Button
                      :label="doc.documentName"
                      icon="pi pi-download"
                      size="small"
                      text
                      :loading="busyDocId === doc.id"
                      class="file-name-button"
                      @click="downloadFile(doc)"
                    />
                    <span class="lab-muted file-size">{{ formatBytes(doc.file?.sizeBytes) }}</span>
                  </div>
                </template>
              </Column>
              <Column
                header=""
                style="width: 13rem"
              >
                <template #body="{ data: doc }">
                  <div class="doc-actions">
                    <Button
                      v-if="doc.status === 'FOR_REVIEW'"
                      label="Approve"
                      size="small"
                      text
                      @click="reviewDoc('approve', doc)"
                    />
                    <Button
                      v-if="doc.status === 'FOR_REVIEW'"
                      label="Reject"
                      size="small"
                      text
                      severity="danger"
                      @click="reviewDoc('reject', doc)"
                    />
                  </div>
                </template>
              </Column>
            </DataTable>
          </TabPanel>

          <TabPanel value="git-certificates">
            <GitCertificatesTab
              :context="tenantStore.context"
              :organization-id="selected.id"
              @changed="refreshDetail"
            />
          </TabPanel>

          <!-- Vetting (BR-TP37/BR-TP38) -->
          <TabPanel value="vetting">
            <p
              v-if="!selectedProfile?.hasProfile"
              class="lab-muted"
            >
              No transporter profile yet. The profile is created when vetting starts — until then there is no vetting
              state to show, which is a well-formed answer rather than an error (BR-TP37).
            </p>
            <template v-else>
              <div class="vetting-steps">
                <div
                  v-for="(step, i) in VETTING_STEPS"
                  :key="step.key"
                  class="vstep"
                  :class="{
                    done: i < vettingIndex,
                    current: i === vettingIndex && !vettingFailure,
                    failed: i === vettingIndex && !!vettingFailure,
                  }"
                >
                  <span class="vstep-dot">
                    <i
                      v-if="i === vettingIndex && !!vettingFailure"
                      class="pi pi-times"
                    />
                    <i
                      v-else-if="i < vettingIndex"
                      class="pi pi-check"
                    />
                    <template v-else>{{ i + 1 }}</template>
                  </span>
                  <span class="vstep-label">
                    {{ i === vettingIndex && vettingFailure ? vettingFailure.label : step.label }}
                  </span>
                </div>
              </div>

              <p
                v-if="isRejected"
                class="lab-muted hint"
              >
                Rejection is a terminal outcome of review, not a stage after it — so it is shown as the review step
                failing rather than as a fourth step. Resubmitting starts a fresh vetting attempt (BR-TP26).
              </p>

              <p
                v-if="isLapsed"
                class="lab-muted hint"
              >
                Goods-in-transit cover lapsed, so this transporter left Vetted and its organization was suspended
                (BR-TP63). There is no direct route back: renewed cover is a new document, reviewed like any other,
                and re-vetting starts a fresh attempt (BR-TP26).
              </p>

              <div class="gate-grid">
                <div class="gate">
                  <span class="gate-label">Attempt</span>
                  <!-- attemptNumber is 0 on a profile that exists but has never
                       had vetting started, which "#0" reads as a first attempt
                       that went nowhere rather than as "none yet". -->
                  <span class="gate-value">
                    {{ selectedProfile.profile.attemptNumber > 0 ? `#${selectedProfile.profile.attemptNumber}` : 'Not started' }}
                  </span>
                </div>
                <div class="gate">
                  <span class="gate-label">Fleet availability gate</span>
                  <Tag
                    :severity="selectedProfile.profile.fleetAvailabilityGate ? 'success' : 'secondary'"
                    :value="selectedProfile.profile.fleetAvailabilityGate ? 'Open' : 'Closed'"
                  />
                </div>
                <div class="gate">
                  <span class="gate-label">GIT verified</span>
                  <Tag
                    :severity="selectedProfile.profile.gitVerified ? 'success' : 'secondary'"
                    :value="selectedProfile.profile.gitVerified ? 'Yes' : 'No'"
                  />
                </div>
                <div class="gate">
                  <span class="gate-label">GIT cover (derived)</span>
                  <Tag
                    :severity="gitSeverity(gitStatus)"
                    :value="gitStatus"
                  />
                </div>
              </div>

              <p class="lab-muted hint">
                Read from the canonical Postgres projection of the <code>TRANSPORTER</code> event stream, not from
                Temporal — the saga orchestrating this is invisible to the browser by design (ADR-047/ADR-049).
                <code>GIT cover</code> is derived per read from the current goods-in-transit documents and is never
                stored (BR-TP38), so it cannot disagree with the Documents tab.
              </p>

              <div
                v-if="selectedProfile.profile.documentReviews"
                class="review-list"
              >
                <span class="section-title">Document Reviews</span>
                <div
                  v-for="(status, ref_) in selectedProfile.profile.documentReviews"
                  :key="ref_"
                  class="review-row"
                >
                  <code>{{ ref_ }}</code>
                  <Tag
                    :severity="status === 'Approved' ? 'success' : status === 'Rejected' ? 'danger' : 'secondary'"
                    :value="status"
                  />
                </div>
              </div>

              <span class="section-title">Audit Trail</span>
              <DataTable
                :value="auditEvents"
                size="small"
                class="sub-table"
              >
                <template #empty>
                  <span class="lab-muted">No audit events yet.</span>
                </template>
                <Column
                  header="Action"
                  field="action"
                />
                <Column
                  header="Actor"
                  field="actor"
                />
                <Column header="Detail">
                  <template #body="{ data: e }">
                    {{ e.metadata?.reason ?? (e.metadata?.version ? `v${e.metadata.version}` : '') }}
                  </template>
                </Column>
                <Column header="When">
                  <template #body="{ data: e }">
                    {{ formatDate(e.createdAt) }}
                  </template>
                </Column>
              </DataTable>
            </template>
          </TabPanel>

          <!-- Rate Sheets — stub by design -->
          <!-- Operating Areas (BR-TP46-BR-TP50) -->
          <TabPanel value="areas">
            <div class="areas-layout">
              <div class="areas-map-col">
                <OperatingAreaMap
                  :assigned="[...assignedCodes]"
                  :busy="areaBusy"
                  @toggle="(code, country) => toggleArea('REGION', code, country)"
                />
                <p class="lab-muted areas-hint">
                  Click a region to add or remove it. The list beside the map is the same selection — it is exact,
                  keyboard-reachable and readable without a pointer, so neither view is a fallback for the other.
                </p>
              </div>

              <div class="areas-list-col">
                <div
                  v-for="group in regionsByCountry"
                  :key="group.country"
                  class="area-group"
                >
                  <div class="area-group-head">
                    <label class="area-row area-row-country">
                      <input
                        type="checkbox"
                        :checked="assignedCodes.has(group.country)"
                        :disabled="areaBusy"
                        @change="toggleArea('COUNTRY', group.country, group.country)"
                      >
                      <span class="area-code">{{ group.country }}</span>
                      <span class="area-name">Entire country</span>
                    </label>
                    <span
                      v-if="areaDisabledReason('COUNTRY', group.country, group.country)"
                      class="area-blocked"
                    >{{ areaDisabledReason('COUNTRY', group.country, group.country) }}</span>
                  </div>

                  <label
                    v-for="region in group.regions"
                    :key="region.code"
                    class="area-row"
                    :class="{ 'area-row-disabled': !!areaDisabledReason('REGION', region.code, region.country) }"
                  >
                    <input
                      type="checkbox"
                      :checked="assignedCodes.has(region.code)"
                      :disabled="areaBusy || (!assignedCodes.has(region.code) && !!areaDisabledReason('REGION', region.code, region.country))"
                      @change="toggleArea('REGION', region.code, region.country)"
                    >
                    <span class="area-code">{{ region.code }}</span>
                    <span class="area-name">{{ region.name }}</span>
                  </label>
                </div>

                <p
                  v-if="!regionCorpus.length"
                  class="lab-muted"
                >
                  No region corpus found. Seed it with
                  <code>go run ./cmd/seed-regions</code> in refdata-service.
                </p>
              </div>
            </div>
          </TabPanel>

          <!-- Tracking Credentials (BR-TP51-BR-TP55) -->
          <TabPanel value="tracking">
            <div class="tracking-layout">
              <DataTable
                :value="trackingCredentials"
                data-key="provider"
                class="partners-table"
              >
                <Column
                  field="provider"
                  header="Provider"
                />
                <Column
                  field="credentialType"
                  header="Type"
                />
                <Column header="Status">
                  <template #body="{ data }">
                    <span :class="['lab-badge', data.credentialsConfigured ? 'lab-badge-ok' : 'lab-badge-muted']">
                      {{ data.credentialsConfigured ? 'Configured' : 'Not configured' }}
                    </span>
                  </template>
                </Column>
                <template #empty>
                  <span class="lab-muted">No tracking providers configured.</span>
                </template>
              </DataTable>

              <div class="cred-form">
                <h4>Configure a provider</h4>
                <div class="cred-note">
                  <i class="pi pi-lock" />
                  <p class="lab-muted">
                    Credentials are encrypted by the service before storage and
                    <strong>cannot be read back</strong> — not by this screen, not by any API. Re-entering a value
                    replaces it. The table above can only ever show <em>that</em> a provider is configured, never
                    what with.
                  </p>
                </div>

                <div class="cred-fields">
                  <Select
                    v-model="credentialForm.provider"
                    :options="PROVIDERS"
                    placeholder="Provider"
                    class="cred-field"
                  />
                  <Select
                    v-model="credentialForm.credentialType"
                    :options="CREDENTIAL_TYPES"
                    placeholder="Credential type"
                    class="cred-field"
                  />
                  <InputText
                    v-model="credentialForm.payload"
                    type="password"
                    autocomplete="new-password"
                    :placeholder="credentialForm.credentialType === 'METADATA_ONLY' ? 'Not required for METADATA_ONLY' : 'Credential value'"
                    :disabled="credentialForm.credentialType === 'METADATA_ONLY'"
                    class="cred-field cred-payload"
                  />
                  <Button
                    label="Store"
                    icon="pi pi-lock"
                    :disabled="!credentialValid || credentialBusy"
                    :loading="credentialBusy"
                    @click="saveCredential"
                  />
                </div>
              </div>
            </div>
          </TabPanel>

          <TabPanel value="rates">
            <div class="empty-tab">
              <i class="pi pi-table" />
              <span class="empty-title">Rate Sheets</span>
              <p class="lab-muted">
                No backend by design. Rate sheets are a separate pricing concern and are out of scope for Phase 38 —
                this tab exists so the navigation matches the intended shape rather than hiding a section that
                belongs here.
              </p>
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </template>

    <!-- ── Registration wizard (BR-TP35) ────────────────────────────────── -->
    <Dialog
      v-model:visible="wizardOpen"
      header="Register Transporter"
      modal
      :style="{ width: '38rem' }"
    >
      <Stepper
        v-model:value="wizardStep"
        linear
      >
        <StepList>
          <Step value="1">
            Company Information
          </Step>
          <Step value="2">
            Fleet
          </Step>
          <Step value="3">
            Documents
          </Step>
        </StepList>
        <StepPanels>
          <StepPanel
            v-slot="{ activateCallback }"
            value="1"
          >
            <div class="form-grid">
              <div class="form-field">
                <label for="wz-name">Name</label>
                <InputText
                  id="wz-name"
                  v-model="wizardForm.name"
                  placeholder="e.g. Acme Trucking"
                  autofocus
                />
              </div>
              <div class="form-field">
                <label for="wz-trading-as">Trading As</label>
                <InputText
                  id="wz-trading-as"
                  v-model="wizardForm.tradingAs"
                />
              </div>
              <div class="form-field">
                <label for="wz-company">Company Name</label>
                <InputText
                  id="wz-company"
                  v-model="wizardForm.companyName"
                />
              </div>
              <div class="form-field">
                <label for="wz-reg">Registration No</label>
                <InputText
                  id="wz-reg"
                  v-model="wizardForm.registrationNo"
                />
              </div>
              <div class="form-field">
                <label for="wz-vat">VAT Registration No</label>
                <InputText
                  id="wz-vat"
                  v-model="wizardForm.vatRegistrationNo"
                />
              </div>
            </div>
            <p class="lab-muted hint">
              Only <strong>Name</strong> is required. Continuing registers the transporter immediately — the next two
              steps add to it, so cancelling after this point leaves a registered transporter behind rather than
              discarding one.
            </p>
            <p
              v-if="wizardError"
              class="error-text"
            >
              {{ wizardError }}
            </p>
            <div class="tab-actions">
              <Button
                label="Cancel"
                text
                size="small"
                @click="wizardOpen = false"
              />
              <Button
                label="Register & Continue"
                size="small"
                :loading="wizardSaving"
                :disabled="!wizardForm.name"
                @click="wizardRegister(activateCallback)"
              />
            </div>
          </StepPanel>

          <StepPanel
            v-slot="{ activateCallback }"
            value="2"
          >
            <p class="lab-muted hint">
              <strong>{{ wizardPartner?.name }}</strong> is registered. Add fleet assets now or skip — they can be
              added at any time from the Fleet tab.
            </p>
            <div class="form-grid">
              <div class="form-field">
                <label for="wz-fleet-reg">Registration No</label>
                <InputText
                  id="wz-fleet-reg"
                  v-model="wizardFleet.registrationNo"
                  placeholder="e.g. CA123456"
                />
              </div>
              <div class="form-field">
                <label for="wz-fleet-vtc">Vehicle Type Code</label>
                <InputText
                  id="wz-fleet-vtc"
                  v-model="wizardFleet.vehicleTypeCode"
                  placeholder="e.g. TAUTLINER"
                />
              </div>
              <div class="form-field">
                <label for="wz-fleet-make">Make</label>
                <InputText
                  id="wz-fleet-make"
                  v-model="wizardFleet.make"
                />
              </div>
              <div class="form-field">
                <label for="wz-fleet-model">Model</label>
                <InputText
                  id="wz-fleet-model"
                  v-model="wizardFleet.model"
                />
              </div>
              <div class="form-field">
                <label for="wz-fleet-vin">VIN</label>
                <InputText
                  id="wz-fleet-vin"
                  v-model="wizardFleet.vin"
                />
              </div>
            </div>
            <p
              v-if="wizardFleetAdded.length"
              class="added-line"
            >
              Added: <code>{{ wizardFleetAdded.join(', ') }}</code>
            </p>
            <p
              v-if="wizardError"
              class="error-text"
            >
              {{ wizardError }}
            </p>
            <div class="tab-actions">
              <Button
                label="Add another"
                icon="pi pi-plus"
                text
                size="small"
                :loading="wizardSaving"
                :disabled="!wizardFleet.registrationNo || !wizardFleet.vehicleTypeCode"
                @click="wizardAddFleet"
              />
              <Button
                label="Continue"
                size="small"
                @click="activateCallback('3')"
              />
            </div>
          </StepPanel>

          <StepPanel value="3">
            <p class="lab-muted hint">
              Register the four shared compliance-document types by dropping the document itself — its file name
              becomes the document name (BR-TP74). GIT certificates use their dedicated tab.
            </p>
            <div class="form-grid">
              <div class="form-field">
                <label for="wz-doc-type">Type</label>
                <Select
                  id="wz-doc-type"
                  v-model="wizardDoc.type"
                  :options="DOCUMENT_TYPES"
                />
              </div>
              <div class="form-field">
                <label>Document</label>
                <input
                  ref="wizardDocFileInput"
                  type="file"
                  accept="application/pdf,image/*"
                  class="hidden-file-input"
                  @change="acceptWizardDocFile($event.target.files?.[0])"
                >
                <button
                  class="doc-drop-zone"
                  type="button"
                  @click="chooseWizardDocFile"
                  @dragover.prevent
                  @drop.prevent="acceptWizardDocFile($event.dataTransfer?.files?.[0])"
                >
                  <span class="drop-title">{{ wizardDoc.file ? wizardDoc.file.name : 'Drop a file here, or click to choose' }}</span>
                  <span class="drop-copy">PDF or image, up to {{ maxFileMb }} MB.</span>
                </button>
              </div>
            </div>
            <p
              v-if="wizardDocsAdded.length"
              class="added-line"
            >
              Added: <code>{{ wizardDocsAdded.join(', ') }}</code>
            </p>
            <p
              v-if="wizardError"
              class="error-text"
            >
              {{ wizardError }}
            </p>
            <div class="tab-actions">
              <Button
                label="Add another"
                icon="pi pi-plus"
                text
                size="small"
                :loading="wizardSaving"
                :disabled="!wizardDoc.type || !wizardDoc.file"
                @click="wizardAddDoc"
              />
              <Button
                label="Finish"
                size="small"
                @click="finishWizard"
              />
            </div>
          </StepPanel>
        </StepPanels>
      </Stepper>
    </Dialog>

    <!-- ── Dialogs ──────────────────────────────────────────────────────── -->
    <Dialog
      v-model:visible="suspendOpen"
      :header="selected ? `Suspend — ${selected.name}` : 'Suspend'"
      modal
      :style="{ width: '26rem' }"
    >
      <div class="form-field">
        <label for="tr-suspend-reason">Reason</label>
        <Textarea
          id="tr-suspend-reason"
          v-model="suspendReason"
          rows="3"
          placeholder="Required — recorded in the audit trail"
          autofocus
        />
      </div>
      <p
        v-if="suspendError"
        class="error-text"
      >
        {{ suspendError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="suspendOpen = false"
        />
        <Button
          label="Suspend"
          severity="danger"
          :loading="suspendSaving"
          :disabled="!suspendReason"
          @click="submitSuspend"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="addDocOpen"
      header="Add Document"
      modal
      :style="{ width: '26rem' }"
    >
      <div class="form-field">
        <label for="tr-doc-type">Type</label>
        <Select
          id="tr-doc-type"
          v-model="addDocForm.type"
          :options="DOCUMENT_TYPES"
        />
      </div>
      <div class="form-field">
        <label>Document</label>
        <button
          class="doc-drop-zone"
          type="button"
          @click="chooseAddDocFile"
          @dragover.prevent
          @drop.prevent="acceptAddDocFile($event.dataTransfer?.files?.[0])"
        >
          <span class="drop-title">{{ addDocForm.file ? addDocForm.file.name : 'Drop a file here, or click to choose' }}</span>
          <span class="drop-copy">PDF or image, up to {{ maxFileMb }} MB. The file name becomes the document name.</span>
        </button>
      </div>
      <p class="lab-muted hint">
        Adding a document of a type that already has a current one supersedes the incumbent rather than replacing it —
        the old row is retained (BR-TP30).
      </p>
      <p
        v-if="addDocError"
        class="error-text"
      >
        {{ addDocError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="addDocOpen = false"
        />
        <Button
          label="Add"
          :loading="addDocSaving"
          :disabled="!addDocForm.type || !addDocForm.file"
          @click="submitAddDocument"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="addFleetOpen"
      header="Add Fleet Asset"
      modal
      :style="{ width: '28rem' }"
    >
      <div class="form-field">
        <label for="tr-fleet-reg">Registration No</label>
        <InputText
          id="tr-fleet-reg"
          v-model="addFleetForm.registrationNo"
          placeholder="e.g. CA123456"
          autofocus
        />
      </div>
      <div class="form-grid">
        <div class="form-field">
          <label for="tr-fleet-make">Make</label>
          <InputText
            id="tr-fleet-make"
            v-model="addFleetForm.make"
          />
        </div>
        <div class="form-field">
          <label for="tr-fleet-model">Model</label>
          <InputText
            id="tr-fleet-model"
            v-model="addFleetForm.model"
          />
        </div>
      </div>
      <div class="form-field">
        <label for="tr-fleet-vin">VIN</label>
        <InputText
          id="tr-fleet-vin"
          v-model="addFleetForm.vin"
        />
      </div>
      <div class="form-field">
        <label for="tr-fleet-vtc">Vehicle Type Code</label>
        <InputText
          id="tr-fleet-vtc"
          v-model="addFleetForm.vehicleTypeCode"
          placeholder="e.g. TAUTLINER"
        />
      </div>
      <p
        v-if="addFleetError"
        class="error-text"
      >
        {{ addFleetError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="addFleetOpen = false"
        />
        <Button
          label="Add"
          :loading="addFleetSaving"
          :disabled="!addFleetForm.registrationNo || !addFleetForm.vehicleTypeCode"
          @click="submitAddFleetAsset"
        />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.transporter-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header,
.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.error-text {
  margin: 0.25rem 0 0;
  color: var(--p-red-400, #f87171);
  font-size: 0.85rem;
}
.hint {
  margin: 0.5rem 0 0;
  font-size: 0.8rem;
  max-width: 74ch;
}

/* Drill-in chrome */
.crumb {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.85rem;
}
.crumb-current {
  font-weight: 600;
}
.detail-ident {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.detail-name {
  font-size: 1.05rem;
  font-weight: 600;
}
.detail-badges {
  display: flex;
  gap: 0.35rem;
  flex-wrap: wrap;
}

/* Forms */
.form-field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-bottom: 0.75rem;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 0.75rem;
}
.tab-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.4rem;
  margin-top: 0.75rem;
}
.tab-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  margin-bottom: 0.6rem;
  font-size: 0.8rem;
}

/* BR-TP39 conflict banner */
.conflict-banner {
  border: 1px solid var(--p-amber-500, #9a7b1e);
  background: color-mix(in srgb, var(--p-amber-500, #9a7b1e) 12%, transparent);
  border-radius: 4px;
  padding: 0.6rem 0.75rem;
  margin-bottom: 0.9rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.conflict-head {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.85rem;
}
.conflict-body {
  margin: 0;
  font-size: 0.8rem;
  color: var(--lab-text-muted, #b7bcc2);
  max-width: 74ch;
}
.conflict-actions {
  display: flex;
  gap: 0.4rem;
  margin-top: 0.15rem;
}

/* Deferred-capability note (38c-ii) */
.deferred-note {
  display: flex;
  align-items: flex-start;
  gap: 0.45rem;
  border: 1px dashed var(--lab-border, #4a515b);
  border-radius: 4px;
  padding: 0.5rem 0.65rem;
  margin-bottom: 0.7rem;
  font-size: 0.8rem;
  color: var(--lab-text-muted, #b7bcc2);
  max-width: 90ch;
}

/* Document files (38c-ii) */
.hidden-file-input {
  display: none;
}

.file-error {
  display: flex;
  align-items: flex-start;
  gap: 0.45rem;
  border: 1px solid var(--p-red-500, #f87171);
  border-radius: 4px;
  padding: 0.5rem 0.65rem;
  margin-bottom: 0.7rem;
  font-size: 0.8rem;
  color: var(--p-red-400, #f87171);
  max-width: 90ch;
}

.file-cell {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  min-width: 0;
}

/* A real filename can be far longer than the column; truncate it rather than
   let it widen the table, keeping the size legible beside it. */
.file-name-button :deep(.p-button-label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 9rem;
}

.file-size {
  font-size: 0.72rem;
  white-space: nowrap;
}

/* Vetting stepper */
.vetting-steps {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  margin-bottom: 0.5rem;
}
.vstep {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.85rem;
  color: var(--lab-text-muted, #b7bcc2);
}
.vstep + .vstep::before {
  content: '';
  width: 1.75rem;
  height: 1px;
  background: var(--lab-border, #4a515b);
  margin-right: 0.35rem;
}
.vstep-dot {
  width: 1.35rem;
  height: 1.35rem;
  border-radius: 50%;
  border: 1px solid var(--lab-border, #4a515b);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 0.7rem;
  flex: 0 0 auto;
}
.vstep.done .vstep-dot,
.vstep.current .vstep-dot {
  border-color: var(--lab-accent);
  color: var(--lab-accent);
}
.vstep.current .vstep-label,
.vstep.done .vstep-label {
  color: var(--lab-text, #dee0e3);
}
.vstep.current .vstep-dot {
  background: color-mix(in srgb, var(--lab-accent) 18%, transparent);
}
.vstep.failed .vstep-dot {
  border-color: var(--p-red-400, #f87171);
  color: var(--p-red-400, #f87171);
}
.vstep.failed .vstep-label {
  color: var(--p-red-400, #f87171);
}

.gate-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  gap: 0.5rem;
  margin: 0.75rem 0 0;
}
.gate {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--lab-border, #4a515b);
  border-radius: 4px;
  background: var(--lab-nested-bg);
}
.gate-label {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--lab-text-muted, #b7bcc2);
}
.gate-value {
  font-weight: 600;
}

.section-title {
  display: block;
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--lab-accent);
  margin: 1rem 0 0.4rem;
}
.review-list {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.review-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.82rem;
}

.empty-tab {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.4rem;
  padding: 2rem 1rem;
  text-align: center;
}
.empty-tab i {
  font-size: 1.5rem;
  color: var(--lab-text-muted, #b7bcc2);
}
.empty-title {
  font-weight: 600;
}
.empty-tab p {
  margin: 0;
  max-width: 60ch;
  font-size: 0.82rem;
}

.added-line {
  margin: 0.25rem 0 0;
  font-size: 0.8rem;
}
.sub-table {
  width: 100%;
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-nested-bg) 95%, var(--lab-accent) 5%);
}
.area-codes { display: flex; flex-wrap: wrap; gap: 0.25rem; align-items: center; }

.mono-col {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.8rem;
}
.doc-actions {
  display: flex;
  gap: 0.25rem;
}

/* --- Operating Areas (BR-TP46-BR-TP50) --- */
.areas-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(260px, 1fr);
  gap: 16px;
  align-items: start;
}

@media (max-width: 1000px) {
  .areas-layout {
    grid-template-columns: 1fr;
  }
}

.areas-hint {
  margin: 0;
  font-size: 12px;
}

.areas-list-col {
  max-height: 480px;
  overflow-y: auto;
  padding-right: 4px;
}

.area-group + .area-group {
  margin-top: 14px;
}

.area-group-head {
  border-bottom: 1px solid var(--lab-border, #4a515b);
  padding-bottom: 4px;
  margin-bottom: 4px;
}

.area-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 2px;
  cursor: pointer;
  font-size: 13px;
}

.area-row-country {
  font-weight: 600;
}

.area-row-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.area-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: var(--lab-muted, #b7bcc2);
  min-width: 58px;
}

.area-blocked {
  display: block;
  font-size: 11px;
  color: var(--lab-warn, #9a7b1e);
  padding-left: 24px;
}

/* --- Tracking Credentials (BR-TP51-BR-TP55) --- */
.tracking-layout {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.cred-form h4 {
  margin: 0 0 6px;
  font-size: 13px;
}

.cred-note {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-bottom: 10px;
  max-width: 70ch;
}

/* The flex lives on the wrapper, not the paragraph: with display:flex on a
   text element every inline child (<strong>, <em>) becomes its own flex
   item, which shredded this sentence into columns. */
.cred-note p {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
}

.cred-note > i {
  margin-top: 2px;
}

.cred-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.cred-field {
  min-width: 180px;
}

.cred-payload {
  min-width: 260px;
}

/* Phase 40 drop zone — the same affordance GitCertificatesTab uses, because
   it is the same gesture: a compliance document is a dropped file. */
.doc-drop-zone {
  min-height: 6rem;
  border: 1px dashed var(--lab-border, #4a515b);
  border-radius: 4px;
  background: transparent;
  color: var(--lab-text, #dee0e3);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  cursor: pointer;
  font: inherit;
  padding: 0.75rem;
  text-align: center;
}
.doc-drop-zone:hover, .doc-drop-zone:focus-visible {
  border-color: var(--lab-accent);
  outline: none;
  background: color-mix(in srgb, var(--lab-accent) 5%, transparent);
}
.doc-drop-zone .drop-title { color: var(--lab-accent); font-weight: 600; }
.doc-drop-zone .drop-copy { color: var(--lab-text-muted, #b7bcc2); font-size: 0.78rem; }
</style>
