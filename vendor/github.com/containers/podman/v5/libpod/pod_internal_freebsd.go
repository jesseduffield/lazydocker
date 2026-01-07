//go:build !remote

package libpod

func (p *Pod) platformRefresh() error {
	return nil
}
