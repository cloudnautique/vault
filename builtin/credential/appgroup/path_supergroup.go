package appgroup

import (
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/vault/helper/policyutil"
	"github.com/hashicorp/vault/helper/strutil"
	"github.com/hashicorp/vault/logical"
	"github.com/hashicorp/vault/logical/framework"
)

// superGroupStorageEntry stores all the options that are set during SecretID
// creation in "supergroup" mode.
type superGroupStorageEntry struct {
	// UUID that uniquely represents this supergroup
	SelectorID string `json:"selector_id" structs:"selector_id" mapstructure:"selector_id"`

	// UUID that serves as the HMAC key for the hashing the 'secret_id's of the App
	HMACKey string `json:"hmac_key" structs:"hmac_key" mapstructure:"hmac_key"`

	// All the Groups that are to be accessible by the SecretID created
	Groups []string `json:"groups" structs:"groups" mapstructure:"groups"`

	// All the Apps that are to be accessible by the SecretID created
	Apps []string `json:"apps" structs:"apps" mapstructure:"apps"`

	// Number of times the generated SecretID can be used to perform login
	SecretIDNumUses int `json:"secret_id_num_uses" structs:"secret_id_num_uses" mapstructure:"secret_id_num_uses"`

	// Duration (less than the backend mount's max TTL) after which a SecretID generated will expire
	SecretIDTTL time.Duration `json:"secret_id_ttl" structs:"secret_id_ttl" mapstructure:"secret_id_ttl"`

	// Duration before which an issued token must be renewed
	TokenTTL time.Duration `json:"token_ttl" structs:"token_ttl" mapstructure:"token_ttl"`

	// Duration after which an issued token should not be allowed to be renewed
	TokenMaxTTL time.Duration `json:"token_max_ttl" structs:"token_max_ttl" mapstructure:"token_max_ttl"`

	// A constraint to require 'secret_id' credential during login
	BindSecretID bool `json:"bind_secret_id" structs:"bind_secret_id" mapstructure:"bind_secret_id"`

	// Along with the combined set of Apps' and Groups' policies, the policies in this
	// list will be added to capabilities of the token issued, when a SecretID generated
	// in superGroup mode is used perform the login.
	AdditionalPolicies []string `json:"additional_policies" structs:"additional_policies" mapstructure:"additional_policies"`
}

// superGroupPaths creates the paths that are used to create SecretIDs in superGroup mode
//
// Paths returned:
// supergroup/secret-id
// supergroup/custom-secret-id
func superGroupPaths(b *backend) []*framework.Path {
	return []*framework.Path{
		&framework.Path{
			Pattern: "supergroup/secret-id$",
			Fields: map[string]*framework.FieldSchema{
				"groups": &framework.FieldSchema{
					Type:        framework.TypeString,
					Default:     "",
					Description: "Comma separated list of Groups.",
				},
				"apps": &framework.FieldSchema{
					Type:        framework.TypeString,
					Default:     "",
					Description: "Comma separated list of Apps.",
				},
				"bind_secret_id": &framework.FieldSchema{
					Type:        framework.TypeBool,
					Default:     true,
					Description: "Impose secret_id to be presented during login using this supergroup. Defaults to 'true'.",
				},
				"additional_policies": &framework.FieldSchema{
					Type:    framework.TypeString,
					Default: "",
					Description: `Comma separated list of policies for the Group. The SecretID created against the Group,
will have access to the union of all the policies of the Apps. In
addition to those, a set of policies can be assigned using this.
`,
				},
				"secret_id_num_uses": &framework.FieldSchema{
					Type:        framework.TypeInt,
					Description: "Number of times the a SecretID can access the Apps represented by the Group.",
				},
				"secret_id_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued SecretID should expire.",
				},
				"token_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued token should expire.",
				},
				"token_max_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued token should not be allowed to be renewed.",
				},
			},

			Callbacks: map[logical.Operation]framework.OperationFunc{
				logical.UpdateOperation: b.pathSuperGroupSecretIDUpdate,
			},
			HelpSynopsis:    pathSuperGroupSecretIDHelpSys,
			HelpDescription: pathSuperGroupSecretIDHelpDesc,
		},
		&framework.Path{
			Pattern: "supergroup/custom-secret-id$",
			Fields: map[string]*framework.FieldSchema{
				"secret_id": &framework.FieldSchema{
					Type:        framework.TypeString,
					Description: "SecretID of the App.",
				},
				"groups": &framework.FieldSchema{
					Type:        framework.TypeString,
					Description: "Comma separated list of Groups.",
				},
				"apps": &framework.FieldSchema{
					Type:        framework.TypeString,
					Description: "Comma separated list of Apps.",
				},
				"bind_secret_id": &framework.FieldSchema{
					Type:        framework.TypeBool,
					Default:     true,
					Description: "Impose secret_id to be presented during login using this supergroup. Defaults to 'true'.",
				},
				"additional_policies": &framework.FieldSchema{
					Type: framework.TypeString,
					Description: `Comma separated list of policies for the Group. The SecretID created against the Group,
will have access to the union of all the policies of the Apps. In
addition to those, a set of policies can be assigned using this.
`,
				},
				"secret_id_num_uses": &framework.FieldSchema{
					Type:        framework.TypeInt,
					Description: "Number of times the a SecretID can access the Apps represented by the Group.",
				},
				"secret_id_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued SecretID should expire.",
				},
				"token_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued token should expire.",
				},
				"token_max_ttl": &framework.FieldSchema{
					Type:        framework.TypeDurationSecond,
					Description: "Duration in seconds after which the issued token should not be allowed to be renewed.",
				},
			},
			Callbacks: map[logical.Operation]framework.OperationFunc{
				logical.UpdateOperation: b.pathSuperGroupCustomSecretIDUpdate,
			},
			HelpSynopsis:    pathSuperGroupCustomSecretIDHelpSys,
			HelpDescription: pathSuperGroupCustomSecretIDHelpDesc,
		},
	}
}

// setSuperGroupEntry creates a storage entry for the options set on a supergroup.
// During login, this storage entry is referred to determine the capabilities to
// be allowed as part of authentication and authorization.
func (b *backend) setSuperGroupEntry(s logical.Storage, superGroupName string, superGroup *superGroupStorageEntry) error {
	b.superGroupLock.Lock()
	defer b.superGroupLock.Unlock()

	entry, err := logical.StorageEntryJSON("supergroup/"+strings.ToLower(superGroupName), superGroup)
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("failed to create storage entry for supergroup")
	}
	if err = s.Put(entry); err != nil {
		return err
	}

	// Create a selector ID reverse mapping entry for the supergroup
	return b.setSelectorIDEntry(s, superGroup.SelectorID, &selectorIDStorageEntry{
		Type: selectorTypeSuperGroup,
		Name: superGroupName,
	})
}

// deleteSuperGroupEntry deletes the storage associated with the supergroup.
func (b *backend) deleteSuperGroupEntry(s logical.Storage, superGroupName string) error {
	if superGroupName == "" {
		return fmt.Errorf("missing superGroupName")
	}
	b.superGroupLock.Lock()
	defer b.superGroupLock.Unlock()

	return s.Delete("supergroup/" + strings.ToLower(superGroupName))
}

// superGroupEntry is used to read the storage entry containing options that are set
// for the supergroup.
func (b *backend) superGroupEntry(s logical.Storage, superGroupName string) (*superGroupStorageEntry, error) {
	if superGroupName == "" {
		return nil, fmt.Errorf("missing superGroupName")
	}

	var result superGroupStorageEntry

	b.superGroupLock.RLock()
	defer b.superGroupLock.RUnlock()

	if entry, err := s.Get("supergroup/" + strings.ToLower(superGroupName)); err != nil {
		return nil, err
	} else if entry == nil {
		return nil, nil
	} else if err := entry.DecodeJSON(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Path to issue a 'secret_id' on the supergroup
func (b *backend) pathSuperGroupSecretIDUpdate(req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	secretID, err := uuid.GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate SecretID:%s", err)
	}
	return b.handleSuperGroupSecretIDCommon(req, data, secretID)
}

// Path to assign a custom 'secret_id' on the supergroup
func (b *backend) pathSuperGroupCustomSecretIDUpdate(req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	return b.handleSuperGroupSecretIDCommon(req, data, data.Get("secret_id").(string))
}

// A common procedure to register a 'secret_id' against a supergroup. Currently this
// only supports 'bind_secret_id'.
func (b *backend) handleSuperGroupSecretIDCommon(req *logical.Request, data *framework.FieldData, secretID string) (*logical.Response, error) {
	if secretID == "" {
		return logical.ErrorResponse("missing secret_id"), nil
	}

	selectorID, err := uuid.GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to create selector_id: %s\n", err)
	}
	hmacKey, err := uuid.GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to create hmac_key: %s\n", err)
	}
	superGroup := &superGroupStorageEntry{
		SelectorID:         selectorID,
		HMACKey:            hmacKey,
		Groups:             strutil.ParseStrings(data.Get("groups").(string)),
		Apps:               strutil.ParseStrings(data.Get("apps").(string)),
		BindSecretID:       data.Get("bind_secret_id").(bool),
		AdditionalPolicies: policyutil.ParsePolicies(data.Get("additional_policies").(string)),
		SecretIDNumUses:    data.Get("secret_id_num_uses").(int),
		SecretIDTTL:        time.Second * time.Duration(data.Get("secret_id_ttl").(int)),
		TokenTTL:           time.Second * time.Duration(data.Get("token_ttl").(int)),
		TokenMaxTTL:        time.Second * time.Duration(data.Get("token_max_ttl").(int)),
	}

	if len(superGroup.Groups) == 0 && len(superGroup.Apps) == 0 {
		return logical.ErrorResponse("missing groups and/or apps"), nil
	}

	if superGroup.SecretIDNumUses < 0 {
		return logical.ErrorResponse("secret_id_num_uses cannot be negative"), nil
	}

	if superGroup.TokenMaxTTL > time.Duration(0) && superGroup.TokenTTL > superGroup.TokenMaxTTL {
		return logical.ErrorResponse("token_ttl should not be greater than token_max_ttl"), nil
	}

	var resp *logical.Response
	if supergroup.TokenMaxTTL > b.System().MaxLeaseTTL() {
		resp = &logical.Response{}
		resp.AddWarning("token_max_ttl is greater than the backend mount's maximum TTL value; issued tokens' max TTL value will be truncated")
	}

	// Only bind_secret_id is supported now. Check for it.
	if !superGroup.BindSecretID {
		return logical.ErrorResponse("bind_secret_id is not set on the app"), nil
	}

	// Since there is no pre-registration of supergroups, there should be a predictable
	// way to refer to the options set on the supergroup, during login time. Setting the
	// name of the supergroup to be the salted hash of secret_id itself.
	superGroupName := b.salt.SaltID(secretID)

	// Store the entry.
	if err := b.setSuperGroupEntry(req.Storage, superGroupName, superGroup); err != nil {
		return nil, err
	}

	// Currently only one bind is supported. So check if it is set.
	if !superGroup.BindSecretID {
		return logical.ErrorResponse("bind_secret_id is not set"), nil
	}

	if err := b.registerSecretIDEntry(req.Storage, selectorID, secretID, hmacKey, &secretIDStorageEntry{
		SecretIDNumUses: superGroup.SecretIDNumUses,
		SecretIDTTL:     superGroup.SecretIDTTL,
	}); err != nil {
		return nil, fmt.Errorf("failed to store secret ID: %s", err)
	}

	resp.Data = map[string]interface{}{
		"secret_id":   secretID,
		"selector_id": superGroup.SelectorID,
	}

	return resp, nil
}

const pathSuperGroupCustomSecretIDHelpSys = `Assign a SecretID of choice against any combination of
registered App(s) and/or Group(s), with custom options.`

const pathSuperGroupCustomSecretIDHelpDesc = `This option is not recommended unless there is a specific
need to do so. This will assign a client supplied SecretID to be used to
access all the specified Apps and all the participating Apps of all the
specified Groups. The options on this endpoint will supercede all the
options set on App(s)/Group(s). The SecretIDs generated will expire after
a period defined by the 'secret_id_ttl' option and/or the backend mount's
maximum TTL value.`

const pathSuperGroupSecretIDHelpSys = `Generate SecretID against any combination of registered App(s)
and/or Group(s), with custom options.`

const pathSuperGroupSecretIDHelpDesc = `The SecretID generated using this endpoint will be able to
access all the specified Apps and all the participating Apps of all the
specified Groups. The options specified on this endpoint will supercede
all the options set on App(s)/Group(s). The SecretIDs generated will expire
after a period defined by the 'secret_id_ttl' option and/or the backend
mount's maximum TTL value.`
