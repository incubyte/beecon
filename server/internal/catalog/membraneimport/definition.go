package membraneimport

// The types below mirror the finalized Beecon provider-definition format v1
// (server/internal/catalog/definition_v1.go: definitionFileV1 and its
// nested shapes) field-for-field, so yaml.Marshal-ing one of these produces
// exactly the YAML catalog.LoadProviderDefinitions' strict KnownFields
// decode expects. Only the fields this importer actually converts are
// populated; expectedParams, authScheme, and credentialStyle are left for a
// human to add — this importer never invents them.

// todoAuthorizeURL, todoTokenURL, todoUserInfoURL, todoScope, and
// todoBaseURL are the OAuth/baseUrl placeholder values emitted for a
// connector the Slice 3 preset table does not recognize: the loader requires
// these fields non-empty, and a TODO placeholder lets the definition parse
// while making unmistakably clear it is not yet usable.
const (
	todoAuthorizeURL = "TODO://set-authorize-url"
	todoTokenURL     = "TODO://set-token-url"
	todoUserInfoURL  = "TODO://set-user-info-url"
	todoScope        = "TODO-set-scope"
	todoBaseURL      = "TODO://set-base-url"
)

// todoTriggerMethod, todoTriggerPath, todoTriggerRecordsPath,
// todoTriggerRecordIDPath, todoTriggerRecordTimestampPath, and
// todoTriggerPayload* are the Slice 5 poll-mapping placeholders: a Membrane
// trigger flow's abstract collectionKey never supplies a concrete REST poll,
// so every field the loader requires non-empty
// (validateTriggerPollMappingFileV1) gets one of these rather than a guessed
// real value.
const (
	todoTriggerMethod              = "TODO-set-poll-method"
	todoTriggerPath                = "TODO://set-poll-path"
	todoTriggerRecordsPath         = "TODO-set-records-path"
	todoTriggerRecordIDPath        = "TODO-set-record-id-path"
	todoTriggerRecordTimestampPath = "TODO-set-record-timestamp-path"
	todoTriggerPayloadKey          = "TODO-set-payload-field"
	todoTriggerPayloadValue        = "TODO-set-payload-path"
)

// scaffoldBanner is prepended to every emitted definition file (spec's IP/
// legal stance: every emitted definition carries a banner stating the
// output is machine-scaffolded and must be reviewed before use).
const scaffoldBanner = "" +
	"# MACHINE-SCAFFOLDED by `beecon import-membrane` — a migration aid, not a\n" +
	"# runtime path. Review and complete every TODO field (and confirm every\n" +
	"# converted mapping) against the provider's own developer documentation\n" +
	"# before this definition goes live.\n"

type outputDefinitionV1 struct {
	FormatVersion int                     `yaml:"formatVersion"`
	Slug          string                  `yaml:"slug"`
	Name          string                  `yaml:"name"`
	Logo          string                  `yaml:"logo"`
	OAuth         outputOAuthV1           `yaml:"oauth"`
	Mapping       outputProviderMappingV1 `yaml:"mapping"`
	Tools         []outputToolV1          `yaml:"tools"`
	Triggers      []outputTriggerV1       `yaml:"triggers,omitempty"`
}

type outputOAuthV1 struct {
	AuthorizeURL string   `yaml:"authorizeUrl"`
	TokenURL     string   `yaml:"tokenUrl"`
	UserInfoURL  string   `yaml:"userInfoUrl,omitempty"`
	Scopes       []string `yaml:"scopes"`
}

type outputProviderMappingV1 struct {
	BaseURL string `yaml:"baseUrl"`
}

type outputToolV1 struct {
	Slug         string              `yaml:"slug"`
	Name         string              `yaml:"name"`
	Description  string              `yaml:"description,omitempty"`
	InputSchema  map[string]any      `yaml:"inputSchema"`
	OutputSchema map[string]any      `yaml:"outputSchema"`
	Mapping      outputToolMappingV1 `yaml:"mapping"`
}

type outputToolMappingV1 struct {
	Path   string            `yaml:"path"`
	Method string            `yaml:"method"`
	Query  map[string]string `yaml:"query,omitempty"`
}

// outputTriggerV1 is Slice 5's emitted trigger entry: ConfigSchema and
// PayloadSchema are the two fields a Membrane trigger flow supplies cleanly
// (parametersSchema and the data-record-created-trigger node's outputSchema
// respectively); Poll is always a TODO-placeholder block, since Membrane
// models the data source as an abstract collectionKey a flow graph resolves,
// never a concrete REST poll.
type outputTriggerV1 struct {
	Slug          string                     `yaml:"slug"`
	Name          string                     `yaml:"name"`
	Description   string                     `yaml:"description,omitempty"`
	ConfigSchema  map[string]any             `yaml:"configSchema"`
	PayloadSchema map[string]any             `yaml:"payloadSchema"`
	Ingestion     string                     `yaml:"ingestion"`
	Poll          outputTriggerPollMappingV1 `yaml:"poll"`
}

// outputTriggerPollMappingV1 mirrors the loader's triggerPollMappingFileV1
// poll-mapping shape (query/body are optional there and are never emitted
// here, since Slice 5 has no source to translate them from).
type outputTriggerPollMappingV1 struct {
	Method              string            `yaml:"method"`
	Path                string            `yaml:"path"`
	RecordsPath         string            `yaml:"recordsPath"`
	RecordIDPath        string            `yaml:"recordIdPath"`
	RecordTimestampPath string            `yaml:"recordTimestampPath"`
	Payload             map[string]string `yaml:"payload"`
}
