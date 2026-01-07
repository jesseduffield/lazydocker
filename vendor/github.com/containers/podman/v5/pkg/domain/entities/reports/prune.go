package reports

import (
	"encoding/json"
	"errors"
)

type PruneReport struct {
	Id   string `json:"Id"`
	Err  error  `json:"Err,omitempty"`
	Size uint64 `json:"Size"`
}

type pruneReportHelper struct {
	Id   string `json:"Id"`
	Err  string `json:"Err,omitempty"`
	Size uint64 `json:"Size"`
}

func (pr *PruneReport) MarshalJSON() ([]byte, error) {
	helper := pruneReportHelper{
		Id:   pr.Id,
		Size: pr.Size,
	}
	if pr.Err != nil {
		helper.Err = pr.Err.Error()
	}
	return json.Marshal(helper)
}

func (pr *PruneReport) UnmarshalJSON(data []byte) error {
	var helper pruneReportHelper
	if err := json.Unmarshal(data, &helper); err != nil {
		return err
	}

	pr.Id = helper.Id
	pr.Size = helper.Size
	if helper.Err != "" {
		pr.Err = errors.New(helper.Err)
	} else {
		pr.Err = nil
	}
	return nil
}

func PruneReportsIds(r []*PruneReport) []string {
	ids := make([]string, 0, len(r))
	for _, v := range r {
		if v == nil || v.Id == "" {
			continue
		}
		ids = append(ids, v.Id)
	}
	return ids
}

func PruneReportsErrs(r []*PruneReport) []error {
	errs := make([]error, 0, len(r))
	for _, v := range r {
		if v == nil || v.Err == nil {
			continue
		}
		errs = append(errs, v.Err)
	}
	return errs
}

func PruneReportsSize(r []*PruneReport) uint64 {
	size := uint64(0)
	for _, v := range r {
		if v == nil {
			continue
		}
		size += v.Size
	}
	return size
}
