package v1

import (
	"encoding/json"
	"errors"
)

// PrincipalKind classifies the verified sender of a command.
type PrincipalKind string

const (
	PrincipalKindActor   PrincipalKind = "actor"
	PrincipalKindService PrincipalKind = "service"
)

var principalMembers = []string{"tenant", "subject", "kind"}

// Principal is the verified sender of a command as Factory established it.
// Factory stamps it after credential verification; clients never supply it.
// Core validates its shape but does not prove who the sender is.
type Principal struct {
	Tenant  TenantID      `json:"tenant"`
	Subject SubjectID     `json:"subject"`
	Kind    PrincipalKind `json:"kind"`
}

// Validate checks each identity and the known principal kind.
func (p Principal) Validate() error {
	if err := p.Tenant.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "principal.tenant")
	}
	if err := p.Subject.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "principal.subject")
	}
	switch p.Kind {
	case PrincipalKindActor, PrincipalKindService:
		return nil
	default:
		return invalidRequest(RequestValidationCodeInvalidField, "principal.kind")
	}
}

// MarshalJSON validates p and preserves tenant, subject, kind member order.
func (p Principal) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	type principalWire struct {
		Tenant  TenantID      `json:"tenant"`
		Subject SubjectID     `json:"subject"`
		Kind    PrincipalKind `json:"kind"`
	}
	return json.Marshal(principalWire(p))
}

// UnmarshalJSON strictly decodes a principal and refuses unknown members.
func (p *Principal) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, principalMembers...)
	if err != nil {
		return principalFieldError(err)
	}
	tenant, err := decodeRequiredString(fields, "tenant")
	if err != nil {
		return principalFieldError(err)
	}
	subject, err := decodeRequiredString(fields, "subject")
	if err != nil {
		return principalFieldError(err)
	}
	kind, err := decodeRequiredString(fields, "kind")
	if err != nil {
		return principalFieldError(err)
	}
	decoded := Principal{Tenant: TenantID(tenant), Subject: SubjectID(subject), Kind: PrincipalKind(kind)}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

func principalFieldError(err error) error {
	var validation *RequestValidationError
	if !errors.As(err, &validation) {
		return invalidRequest(RequestValidationCodeInvalidField, "principal")
	}
	code := validation.Code
	if code == RequestValidationCodeInvalidJSON {
		code = RequestValidationCodeInvalidField
	}
	if validation.Field == "" {
		return invalidRequest(code, "principal")
	}
	return invalidRequest(code, "principal."+validation.Field)
}

// decodeOptionalPrincipal refuses an explicit null member.
func decodeOptionalPrincipal(fields map[string]json.RawMessage) (*Principal, error) {
	raw, ok := fields["principal"]
	if !ok {
		return nil, nil
	}
	if isJSONNull(raw) {
		return nil, invalidRequest(RequestValidationCodeInvalidField, "principal")
	}
	var principal Principal
	if err := principal.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return &principal, nil
}

func validateOptionalPrincipal(principal *Principal) error {
	if principal == nil {
		return nil
	}
	return principal.Validate()
}
