//go:build !linux && !windows

// Package for handling processes and PIDs.
package pidhandle

// Creates new PIDHandle for a given process pid.
func NewPIDHandle(pid int) (PIDHandle, error) {
	h := pidHandle{pid: pid}
	pidData, err := h.String()
	if err != nil {
		return nil, err
	}
	h.pidData = pidData
	return &h, nil
}

// Creates new PIDHandle for a given process pid using the pidData
// originally obtained from PIDHandle.String().
func NewPIDHandleFromString(pid int, pidData string) (PIDHandle, error) {
	return &pidHandle{
		pid:     pid,
		pidData: pidData,
	}, nil
}
