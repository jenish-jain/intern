package orchestrator

import "errors"

var (
	// ErrTransient is a wrapper to mark transient failures (retryable)
	ErrTransient = errors.New("transient")
	// ErrPermanent is a wrapper to mark permanent failures (do not retry)
	ErrPermanent = errors.New("permanent")
)

// MakeTransient wraps an error as transient
func MakeTransient(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrTransient, err)
}

// MakePermanent wraps an error as permanent
func MakePermanent(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrPermanent, err)
}

// IsTransient returns true if error contains ErrTransient
func IsTransient(err error) bool {
	return err != nil && errors.Is(err, ErrTransient)
}

// IsPermanent returns true if error contains ErrPermanent
func IsPermanent(err error) bool {
	return err != nil && errors.Is(err, ErrPermanent)
}
