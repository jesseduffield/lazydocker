package types

import (
	"time"
)

type SecretSpec struct {
	Name   string
	Driver SecretDriverSpec
	Labels map[string]string
}

type SecretVersion struct {
	Index int
}

type SecretDriverSpec struct {
	Name    string
	Options map[string]string
}

type SecretCreateReport struct {
	ID string
}

type SecretListReport struct {
	ID        string
	Name      string
	Driver    string
	CreatedAt string
	UpdatedAt string
}

type SecretRmReport struct {
	ID  string
	Err error
}

type SecretInfoReport struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Spec       SecretSpec
	SecretData string `json:"SecretData,omitempty"`
}

type SecretInfoReportCompat struct {
	SecretInfoReport
	Version SecretVersion
}
