package sdk

// WindowsPipeConfig is a helper structure for configuring named pipe parameters on Windows.
type WindowsPipeConfig struct {
	// SecurityDescriptor contains a Windows security descriptor in SDDL format.
	SecurityDescriptor string

	// InBufferSize in bytes.
	InBufferSize int32

	// OutBufferSize in bytes.
	OutBufferSize int32
}
