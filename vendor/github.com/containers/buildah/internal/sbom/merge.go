package sbom

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/containers/buildah/define"
)

// getComponentNameVersionPurl extracts the "name", "version", and "purl"
// fields of a CycloneDX component record
func getComponentNameVersionPurl(anyComponent any) (string, string, error) {
	if component, ok := anyComponent.(map[string]any); ok {
		// read the "name" field
		anyName, ok := component["name"]
		if !ok {
			return "", "", fmt.Errorf("no name in component %v", anyComponent)
		}
		name, ok := anyName.(string)
		if !ok {
			return "", "", fmt.Errorf("name %v is not a string", anyName)
		}
		// read the optional "version" field
		var version string
		anyVersion, ok := component["version"]
		if ok {
			if version, ok = anyVersion.(string); !ok {
				return "", "", fmt.Errorf("version %v is not a string", anyVersion)
			}
		}
		// combine them
		nameWithVersion := name
		if version != "" {
			nameWithVersion += ("@" + version)
		}
		// read the optional "purl" field
		var purl string
		anyPurl, ok := component["purl"]
		if ok {
			if purl, ok = anyPurl.(string); !ok {
				return "", "", fmt.Errorf("purl %v is not a string", anyPurl)
			}
		}
		return nameWithVersion, purl, nil
	}
	return "", "", fmt.Errorf("component %v is not an object", anyComponent)
}

// getPackageNameVersionInfoPurl extracts the "name", "versionInfo", and "purl"
// fields of an SPDX package record
func getPackageNameVersionInfoPurl(anyPackage any) (string, string, error) {
	if pkg, ok := anyPackage.(map[string]any); ok {
		// read the "name" field
		anyName, ok := pkg["name"]
		if !ok {
			return "", "", fmt.Errorf("no name in package %v", anyPackage)
		}
		name, ok := anyName.(string)
		if !ok {
			return "", "", fmt.Errorf("name %v is not a string", anyName)
		}
		// read the optional "versionInfo" field
		var versionInfo string
		if anyVersionInfo, ok := pkg["versionInfo"]; ok {
			if versionInfo, ok = anyVersionInfo.(string); !ok {
				return "", "", fmt.Errorf("versionInfo %v is not a string", anyVersionInfo)
			}
		}
		// combine them
		nameWithVersionInfo := name
		if versionInfo != "" {
			nameWithVersionInfo += ("@" + versionInfo)
		}
		// now look for optional externalRefs[].purl if "referenceCategory"
		// is "PACKAGE-MANAGER" and "referenceType" is "purl"
		var purl string
		if anyExternalRefs, ok := pkg["externalRefs"]; ok {
			if externalRefs, ok := anyExternalRefs.([]any); ok {
				for _, anyExternalRef := range externalRefs {
					if externalRef, ok := anyExternalRef.(map[string]any); ok {
						anyReferenceCategory, ok := externalRef["referenceCategory"]
						if !ok {
							continue
						}
						if referenceCategory, ok := anyReferenceCategory.(string); !ok || referenceCategory != "PACKAGE-MANAGER" {
							continue
						}
						anyReferenceType, ok := externalRef["referenceType"]
						if !ok {
							continue
						}
						if referenceType, ok := anyReferenceType.(string); !ok || referenceType != "purl" {
							continue
						}
						if anyReferenceLocator, ok := externalRef["referenceLocator"]; ok {
							if purl, ok = anyReferenceLocator.(string); !ok {
								return "", "", fmt.Errorf("purl %v is not a string", anyReferenceLocator)
							}
						}
					}
				}
			}
		}
		return nameWithVersionInfo, purl, nil
	}
	return "", "", fmt.Errorf("package %v is not an object", anyPackage)
}

// getLicenseID extracts the "licenseId" field of an SPDX license record
func getLicenseID(anyLicense any) (string, error) {
	var licenseID string
	if lic, ok := anyLicense.(map[string]any); ok {
		anyID, ok := lic["licenseId"]
		if !ok {
			return "", fmt.Errorf("no licenseId in license %v", anyID)
		}
		id, ok := anyID.(string)
		if !ok {
			return "", fmt.Errorf("licenseId %v is not a string", anyID)
		}
		licenseID = id
	}
	return licenseID, nil
}

// mergeSlicesWithoutDuplicates merges a named slice in "base" with items from
// the same slice in "merge", so long as getKey() returns values for them that
// it didn't for items from the "base" slice
func mergeSlicesWithoutDuplicates(base, merge map[string]any, sliceField string, getKey func(record any) (string, error)) error {
	uniqueKeys := make(map[string]struct{})
	// go through all of the values in the base slice, grab their
	// keys, and note them
	baseRecords := base[sliceField]
	baseRecordsSlice, ok := baseRecords.([]any)
	if !ok {
		baseRecordsSlice = []any{}
	}
	for _, anyRecord := range baseRecordsSlice {
		key, err := getKey(anyRecord)
		if err != nil {
			return err
		}
		uniqueKeys[key] = struct{}{}
	}
	// go through all of the record values in the merge doc, grab their
	// associated keys, and append them to the base records slice if we
	// haven't seen the key yet
	mergeRecords := merge[sliceField]
	mergeRecordsSlice, ok := mergeRecords.([]any)
	if !ok {
		mergeRecordsSlice = []any{}
	}
	for _, anyRecord := range mergeRecordsSlice {
		key, err := getKey(anyRecord)
		if err != nil {
			return err
		}
		if _, present := uniqueKeys[key]; !present {
			baseRecordsSlice = append(baseRecordsSlice, anyRecord)
			uniqueKeys[key] = struct{}{}
		}
	}
	if len(baseRecordsSlice) > 0 {
		base[sliceField] = baseRecordsSlice
	}
	return nil
}

// decodeJSON decodes a file into a map
func decodeJSON(inputFile string, document *map[string]any) error {
	src, err := os.Open(inputFile)
	if err != nil {
		return err
	}
	defer src.Close()
	if err = json.NewDecoder(src).Decode(document); err != nil {
		return fmt.Errorf("decoding JSON document from %q: %w", inputFile, err)
	}
	return nil
}

// encodeJSON encodes a map and saves it to a file
func encodeJSON(outputFile string, document any) error {
	dst, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err = json.NewEncoder(dst).Encode(document); err != nil {
		return fmt.Errorf("writing JSON document to %q: %w", outputFile, err)
	}
	return nil
}

// Merge adds the contents of inputSBOM to inputOutputSBOM using one of a
// handful of named strategies.
func Merge(mergeStrategy define.SBOMMergeStrategy, inputOutputSBOM, inputSBOM, outputPURL string) (err error) {
	type purlImageContents struct {
		Dependencies []string `json:"dependencies,omitempty"`
	}
	type purlDocument struct {
		ImageContents purlImageContents `json:"image_contents"`
	}
	purls := []string{}
	seenPurls := make(map[string]struct{})

	switch mergeStrategy {
	case define.SBOMMergeStrategyCycloneDXByComponentNameAndVersion:
		var base, merge map[string]any
		if err = decodeJSON(inputOutputSBOM, &base); err != nil {
			return fmt.Errorf("reading first SBOM to be merged from %q: %w", inputOutputSBOM, err)
		}
		if err = decodeJSON(inputSBOM, &merge); err != nil {
			return fmt.Errorf("reading second SBOM to be merged from %q: %w", inputSBOM, err)
		}

		// merge the "components" lists based on unique combinations of
		// "name" and "version" fields, and save unique package URL
		// values
		err = mergeSlicesWithoutDuplicates(base, merge, "components", func(anyPackage any) (string, error) {
			nameWithVersion, purl, err := getComponentNameVersionPurl(anyPackage)
			if purl != "" {
				if _, seen := seenPurls[purl]; !seen {
					purls = append(purls, purl)
					seenPurls[purl] = struct{}{}
				}
			}
			return nameWithVersion, err
		})
		if err != nil {
			return fmt.Errorf("merging the %q field of CycloneDX SBOMs: %w", "components", err)
		}

		// save the updated doc
		err = encodeJSON(inputOutputSBOM, base)

	case define.SBOMMergeStrategySPDXByPackageNameAndVersionInfo:
		var base, merge map[string]any
		if err = decodeJSON(inputOutputSBOM, &base); err != nil {
			return fmt.Errorf("reading first SBOM to be merged from %q: %w", inputOutputSBOM, err)
		}
		if err = decodeJSON(inputSBOM, &merge); err != nil {
			return fmt.Errorf("reading second SBOM to be merged from %q: %w", inputSBOM, err)
		}

		// merge the "packages" lists based on unique combinations of
		// "name" and "versionInfo" fields, and save unique package URL
		// values
		err = mergeSlicesWithoutDuplicates(base, merge, "packages", func(anyPackage any) (string, error) {
			nameWithVersionInfo, purl, err := getPackageNameVersionInfoPurl(anyPackage)
			if purl != "" {
				if _, seen := seenPurls[purl]; !seen {
					purls = append(purls, purl)
					seenPurls[purl] = struct{}{}
				}
			}
			return nameWithVersionInfo, err
		})
		if err != nil {
			return fmt.Errorf("merging the %q field of SPDX SBOMs: %w", "packages", err)
		}

		// merge the "hasExtractedLicensingInfos" lists based on unique
		// "licenseId" values
		err = mergeSlicesWithoutDuplicates(base, merge, "hasExtractedLicensingInfos", getLicenseID)
		if err != nil {
			return fmt.Errorf("merging the %q field of SPDX SBOMs: %w", "hasExtractedLicensingInfos", err)
		}

		// save the updated doc
		err = encodeJSON(inputOutputSBOM, base)

	case define.SBOMMergeStrategyCat:
		dst, err := os.OpenFile(inputOutputSBOM, os.O_RDWR|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer dst.Close()
		src, err := os.Open(inputSBOM)
		if err != nil {
			return err
		}
		defer src.Close()
		if _, err = io.Copy(dst, src); err != nil {
			return err
		}
	}
	if err == nil {
		sort.Strings(purls)
		err = encodeJSON(outputPURL, &purlDocument{purlImageContents{Dependencies: purls}})
	}
	return err
}
