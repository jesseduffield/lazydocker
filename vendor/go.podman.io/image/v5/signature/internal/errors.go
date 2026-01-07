package internal

// InvalidSignatureError is returned when parsing an invalid signature.
// This is publicly visible as signature.InvalidSignatureError
type InvalidSignatureError struct {
	msg string
}

func (err InvalidSignatureError) Error() string {
	return err.msg
}

func NewInvalidSignatureError(msg string) InvalidSignatureError {
	return InvalidSignatureError{msg: msg}
}

// JSONFormatToInvalidSignatureError converts JSONFormatError to InvalidSignatureError.
// All other errors are returned as is.
func JSONFormatToInvalidSignatureError(err error) error {
	if formatErr, ok := err.(JSONFormatError); ok {
		err = NewInvalidSignatureError(formatErr.Error())
	}
	return err
}
