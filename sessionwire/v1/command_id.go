package v1

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	commandIDFramePrefix = "looprig-command-id\x00\x01"
	maxUint32            = uint64(1<<32 - 1)
)

// CommandIDFramingCode is a stable machine-readable reason a deterministic
// command ID cannot be framed.
type CommandIDFramingCode string

const (
	// CommandIDFramingCodeTooManyFields reports a field count larger than a
	// uint32 can represent.
	CommandIDFramingCodeTooManyFields CommandIDFramingCode = "too_many_fields"
	// CommandIDFramingCodeFieldTooLong reports a UTF-8 field larger than a
	// uint32 can represent.
	CommandIDFramingCodeFieldTooLong CommandIDFramingCode = "field_too_long"
)

// CommandIDFramingError reports a deterministic command ID input that cannot
// be represented by the version 1 framing. Code is stable for callers that
// need to distinguish framing failures without matching Error.
type CommandIDFramingError struct {
	Code CommandIDFramingCode
}

func (e *CommandIDFramingError) Error() string {
	return "sessionwire/v1: invalid deterministic command ID framing: " + string(e.Code)
}

// DeterministicCommandID derives a canonical CommandID from a domain followed
// by zero or more fields. The domain is field zero in the framed input. All
// fields must be valid UTF-8; their raw UTF-8 bytes, without Unicode
// normalization, determine the result.
func DeterministicCommandID(domain string, fields ...string) (CommandID, error) {
	fieldCount, err := commandIDFrameUint32(uint64(len(fields))+1, CommandIDFramingCodeTooManyFields)
	if err != nil {
		return "", err
	}
	domainLength, err := commandIDFrameFieldLength(domain)
	if err != nil {
		return "", err
	}

	digest := sha256.New()
	_, _ = digest.Write([]byte(commandIDFramePrefix))
	writeCommandIDFrameUint32(digest, fieldCount)
	writeCommandIDFrameField(digest, domain, domainLength)
	for _, field := range fields {
		fieldLength, err := commandIDFrameFieldLength(field)
		if err != nil {
			return "", err
		}
		writeCommandIDFrameField(digest, field, fieldLength)
	}

	return CommandID("v1:" + base64.RawURLEncoding.EncodeToString(digest.Sum(nil))), nil
}

// CanonicalDeadlineUnixMillis returns deadline as a canonical signed count of
// Unix milliseconds: zero is "0", negatives use a single leading '-', and
// positive values have no leading zeroes.
func CanonicalDeadlineUnixMillis(deadline time.Time) string {
	return strconv.FormatInt(deadline.UnixMilli(), 10)
}

func commandIDFrameFieldLength(field string) (uint32, error) {
	switch {
	case !utf8.ValidString(field):
		return 0, &IDValidationError{Code: IDValidationCodeInvalidUTF8}
	default:
		return commandIDFrameUint32(uint64(len(field)), CommandIDFramingCodeFieldTooLong)
	}
}

func commandIDFrameUint32(value uint64, overflowCode CommandIDFramingCode) (uint32, error) {
	if value > maxUint32 {
		return 0, &CommandIDFramingError{Code: overflowCode}
	}
	return uint32(value), nil
}

func writeCommandIDFrameUint32(digest hash.Hash, value uint32) {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], value)
	_, _ = digest.Write(bytes[:])
}

func writeCommandIDFrameField(digest hash.Hash, field string, fieldLength uint32) {
	writeCommandIDFrameUint32(digest, fieldLength)
	_, _ = digest.Write([]byte(field))
}
