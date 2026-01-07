package core

import (
	"github.com/letsencrypt/boulder/identifier"
)

// PolicyAuthority defines the public interface for the Boulder PA
// TODO(#5891): Move this interface to a more appropriate location.
type PolicyAuthority interface {
	WillingToIssue([]string) error
	ChallengesFor(identifier.ACMEIdentifier) ([]Challenge, error)
	ChallengeTypeEnabled(AcmeChallenge) bool
	CheckAuthz(*Authorization) error
}
