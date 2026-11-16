package vsimport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	ia "github.com/brian1917/illumioapi/v2"
	"github.com/brian1917/workloader/cmd/vsexport"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/viper"
)

// Input is the data structure ImportVirtualServicesFromCSV expects
type Input struct {
	PCE                                                        ia.PCE
	ImportFile                                                 string
	ImportData                                                 [][]string
	RemoveValue                                                string
	UpdatePCE, NoPrompt, Provision, IgnoreHref, Create, Update bool
}

const tempLabelPrefix = "vs-import-temp"

// importVS holds the processing state for a single CSV row
type importVS struct {
	input      *Input
	headers    map[string]int
	line       []string
	lineNum    int
	existing   *ia.VirtualService // nil when creating
	target     ia.VirtualService  // payload for create or update
	change     bool
	identifier string
}

// cell returns the value for a header and whether the header is in the CSV.
func (v *importVS) cell(header string) (string, bool) {
	col, ok := v.headers[header]
	if !ok || col >= len(v.line) {
		return "", false
	}
	return strings.TrimSpace(v.line[col]), true
}

// isRemove returns true if the value is the remove value.
func (v *importVS) isRemove(val string) bool {
	return v.input.RemoveValue != "" && val == v.input.RemoveValue
}

func (v *importVS) logChange(format string, a ...any) {
	v.change = true
	utils.LogInfof(false, "csv line %d - %s - %s", v.lineNum, v.identifier, fmt.Sprintf(format, a...))
}

func blankLog(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}

// ImportVirtualServicesFromCSV creates and updates virtual services from a CSV
func ImportVirtualServicesFromCSV(input Input) {

	// Parse the CSV
	data := input.ImportData
	var err error
	if len(data) == 0 {
		data, err = utils.ParseCSV(input.ImportFile)
		if err != nil {
			utils.LogError(err.Error())
		}
	}
	if len(data) < 2 {
		utils.LogInfo("csv has no data rows. nothing to be done.", true)
		return
	}

	utils.LogInfof(false, "vs-import input - file: %s; remove-value: %s; create: %t; update: %t; ignore-href: %t; provision: %t; update-pce: %t; no-prompt: %t",
		input.ImportFile, input.RemoveValue, input.Create, input.Update, input.IgnoreHref, input.Provision, input.UpdatePCE, input.NoPrompt)

	// Load the PCE
	apiResps, err := input.PCE.Load(ia.LoadInput{ProvisionStatus: "draft", VirtualServices: true, Services: true, Labels: true, LabelDimensions: true}, utils.UseMulti())
	utils.LogMultiAPIRespV2(apiResps)
	if err != nil {
		utils.LogError(err.Error())
	}

	// Process headers
	headers := make(map[string]int)
	for i, h := range data[0] {
		headers[strings.TrimSpace(h)] = i
	}

	// Determine label columns. Label key -> column.
	labelDimensions := make(map[string]bool)
	for _, ld := range input.PCE.LabelDimensionsSlice {
		labelDimensions[ld.Key] = true
	}
	reserved := vsexport.ReservedHeaders()
	labelCols := make(map[string]int)
	for h, col := range headers {
		if strings.HasPrefix(h, vsexport.LabelPrefix) {
			key := strings.TrimPrefix(h, vsexport.LabelPrefix)
			if !labelDimensions[key] {
				utils.LogWarningf(true, "%s header is not a label key in the pce. ignoring column.", h)
				continue
			}
			labelCols[key] = col
		} else if !reserved[h] {
			if labelDimensions[h] {
				labelCols[h] = col
			} else {
				utils.LogInfof(false, "%s header is not a vs-import header or label key. ignoring column.", h)
			}
		}
	}
	labelKeys := []string{}
	for k := range labelCols {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)
	utils.LogInfof(false, "label columns: %v", labelKeys)

	_, hasHref := headers[vsexport.HeaderHref]
	_, hasName := headers[vsexport.HeaderName]
	if (!hasHref || input.IgnoreHref) && !hasName {
		utils.LogError("csv requires a name header or an href header (without --ignore-href) for matching")
	}

	var toCreate, toUpdate []*importVS
	newLabels := []ia.Label{}
	csvNames := make(map[string]int)
	matchedHrefs := make(map[string]int)

csvRows:
	for i, line := range data {
		if i == 0 {
			continue
		}
		v := &importVS{input: &input, headers: headers, line: line, lineNum: i + 1}

		// Find the matching virtual service
		href, _ := v.cell(vsexport.HeaderHref)
		name, _ := v.cell(vsexport.HeaderName)
		if href != "" && !input.IgnoreHref {
			existing, ok := input.PCE.VirtualServices[href]
			if !ok || existing.Href != href {
				utils.LogWarningf(true, "csv line %d - %s does not exist in the pce. skipping.", v.lineNum, href)
				continue
			}
			v.existing = &existing
			v.identifier = href
		} else if name != "" {
			v.identifier = name
			if existing, ok := input.PCE.VirtualServices[name]; ok && existing.Name == name {
				v.existing = &existing
			}
		} else {
			utils.LogWarningf(true, "csv line %d - name and href are blank. skipping.", v.lineNum)
			continue
		}

		// Check duplicates in the CSV
		if v.existing != nil {
			if prev, ok := matchedHrefs[v.existing.Href]; ok {
				utils.LogWarningf(true, "csv line %d - %s already matched on csv line %d. skipping.", v.lineNum, v.identifier, prev)
				continue
			}
			matchedHrefs[v.existing.Href] = v.lineNum
			if v.existing.UpdateType == "delete" {
				utils.LogWarningf(true, "csv line %d - %s is pending deletion in the pce. skipping.", v.lineNum, v.identifier)
				continue
			}
			if !input.Update {
				utils.LogInfof(false, "csv line %d - %s exists and update is disabled. skipping.", v.lineNum, v.identifier)
				continue
			}
			v.target = ia.VirtualService{Href: v.existing.Href}
		} else {
			if prev, ok := csvNames[name]; ok {
				utils.LogWarningf(true, "csv line %d - %s is also being created on csv line %d. skipping.", v.lineNum, name, prev)
				continue
			}
			csvNames[name] = v.lineNum
			if !input.Create {
				utils.LogInfof(false, "csv line %d - %s does not exist and create is disabled. skipping.", v.lineNum, v.identifier)
				continue
			}
			v.target = ia.VirtualService{Name: name}
		}

		// Process each field. Any error skips the row.
		for _, process := range []func() error{v.name, v.description, v.service, v.serviceAddresses, v.ipOverrides, v.applyTo, v.externalData} {
			if err := process(); err != nil {
				utils.LogWarningf(true, "csv line %d - %s - %s. skipping.", v.lineNum, v.identifier, err)
				continue csvRows
			}
		}
		newLabels = v.labels(labelKeys, labelCols, newLabels)

		if v.existing == nil {
			if v.target.Service == nil && v.target.ServicePorts == nil {
				utils.LogWarningf(true, "csv line %d - %s - a service or service_ports value is required to create a virtual service. skipping.", v.lineNum, v.identifier)
				continue
			}
			utils.LogInfof(false, "csv line %d - %s to be created", v.lineNum, v.identifier)
			toCreate = append(toCreate, v)
		} else if v.change {
			toUpdate = append(toUpdate, v)
		}
	}

	if len(toCreate) == 0 && len(toUpdate) == 0 {
		utils.LogInfo("nothing to be done.", true)
		return
	}

	utils.LogInfof(true, "workloader identified %d labels to create.", len(newLabels))
	utils.LogInfof(true, "workloader identified %d virtual services to create.", len(toCreate))
	utils.LogInfof(true, "workloader identified %d virtual services to update.", len(toUpdate))

	if !input.UpdatePCE {
		utils.LogInfo("see workloader.log for more details. to do the import, run again using --update-pce flag.", true)
		return
	}

	if !input.NoPrompt {
		var prompt string
		fmt.Printf("\r\n%s [PROMPT] - Do you want to run the import to %s (%s) (yes/no)? ", time.Now().Format("2006-01-02 15:04:05 "), input.PCE.FriendlyName, viper.GetString(input.PCE.FriendlyName+".fqdn"))
		fmt.Scanln(&prompt)
		if strings.ToLower(prompt) != "yes" {
			utils.LogInfo("prompt denied", true)
			return
		}
	}

	// Create the labels and replace the temporary hrefs
	labelReplacements := make(map[string]string)
	for _, l := range newLabels {
		created, api, err := input.PCE.CreateLabel(ia.Label{Key: l.Key, Value: l.Value})
		utils.LogAPIRespV2("CreateLabel", api)
		if err != nil {
			utils.LogError(err.Error())
		}
		labelReplacements[l.Href] = created.Href
		utils.LogInfof(true, "created new %s label - %s - %d", created.Key, created.Value, api.StatusCode)
	}
	for _, v := range append(append([]*importVS{}, toCreate...), toUpdate...) {
		if v.target.Labels == nil {
			continue
		}
		for i, l := range *v.target.Labels {
			if strings.HasPrefix(l.Href, tempLabelPrefix) {
				(*v.target.Labels)[i] = ia.Label{Href: labelReplacements[l.Href]}
			}
		}
	}

	provisionHrefs := []string{}
	var created, updated, failed int

	for _, v := range toCreate {
		vs, api, err := input.PCE.CreateVirtualService(v.target)
		utils.LogAPIRespV2("CreateVirtualService", api)
		if err != nil {
			failed++
			utils.LogWarningf(true, "csv line %d - %s - create failed - status code %d - %s", v.lineNum, v.identifier, api.StatusCode, api.RespBody)
			continue
		}
		created++
		provisionHrefs = append(provisionHrefs, vs.Href)
		utils.LogInfof(true, "csv line %d - %s created - %s - status code %d", v.lineNum, v.identifier, vs.Href, api.StatusCode)
	}

	for _, v := range toUpdate {
		href := v.target.Href
		api, err := input.PCE.UpdateVirtualService(v.target)
		utils.LogAPIRespV2("UpdateVirtualService", api)
		if err != nil {
			failed++
			utils.LogWarningf(true, "csv line %d - %s - update failed - status code %d - %s", v.lineNum, v.identifier, api.StatusCode, api.RespBody)
			continue
		}
		updated++
		provisionHrefs = append(provisionHrefs, href)
		utils.LogInfof(true, "csv line %d - %s updated - status code %d", v.lineNum, v.identifier, api.StatusCode)
	}

	utils.LogInfof(true, "%d virtual services created. %d virtual services updated. %d failed.", created, updated, failed)

	if input.Provision && len(provisionHrefs) > 0 {
		api, err := input.PCE.ProvisionHref(provisionHrefs, "workloader vs-import")
		utils.LogAPIRespV2("ProvisionHref", api)
		if err != nil {
			utils.LogErrorf("provisioning - %s", err)
		}
		utils.LogInfof(true, "provisioning successful for %d virtual services - status code %d", len(provisionHrefs), api.StatusCode)
	}
}

func (v *importVS) name() error {
	val, ok := v.cell(vsexport.HeaderName)
	if !ok || val == "" || v.existing == nil {
		return nil
	}
	if v.isRemove(val) {
		return fmt.Errorf("name cannot be removed")
	}
	if val != v.existing.Name {
		v.logChange("name to be changed from %s to %s", v.existing.Name, val)
		v.target.Name = val
	}
	return nil
}

func (v *importVS) description() error {
	val, ok := v.cell(vsexport.HeaderDescription)
	if !ok || val == "" {
		return nil
	}
	current := ""
	if v.existing != nil {
		current = ia.PtrToVal(v.existing.Description)
	}
	if v.isRemove(val) {
		if current != "" {
			v.logChange("description to be removed")
			v.target.Description = ia.Ptr("")
		}
		return nil
	}
	if val != current {
		if v.existing != nil {
			v.logChange("description to be changed from \"%s\" to \"%s\"", blankLog(current), val)
		}
		v.target.Description = ia.Ptr(val)
	}
	return nil
}

// service processes the service and service_ports columns together since they are mutually exclusive.
func (v *importVS) service() error {
	svcVal, _ := v.cell(vsexport.HeaderService)
	portsVal, _ := v.cell(vsexport.HeaderServicePorts)
	if v.isRemove(svcVal) || v.isRemove(portsVal) {
		return fmt.Errorf("service and service_ports cannot be removed. provide a new value instead")
	}
	if svcVal != "" && portsVal != "" {
		return fmt.Errorf("service and service_ports cannot both be provided")
	}

	if svcVal != "" {
		svc, ok := v.input.PCE.Services[svcVal]
		if !ok {
			return fmt.Errorf("service %s does not exist in the pce", svcVal)
		}
		currentHref := ""
		if v.existing != nil && v.existing.Service != nil {
			currentHref = v.existing.Service.Href
		}
		if svc.Href != currentHref {
			if v.existing != nil {
				currentLog := v.input.PCE.Services[currentHref].Name
				if currentHref == "" {
					currentLog = "service_ports " + strings.Join(vsexport.ServicePortsToString(v.existing.ServicePorts), ";")
				}
				v.logChange("service to be changed from %s to %s", blankLog(currentLog), svc.Name)
			}
			v.target.Service = &ia.Service{Href: svc.Href}
		}
	}

	if portsVal != "" {
		sps, err := parseServicePorts(portsVal)
		if err != nil {
			return err
		}
		if len(sps) == 0 {
			return fmt.Errorf("service_ports has no valid entries")
		}
		current := []ia.ServicePort{}
		if v.existing != nil {
			current = ia.PtrToVal(v.existing.ServicePorts)
		}
		if v.existing == nil || servicePortsKey(sps) != servicePortsKey(current) || v.existing.Service != nil {
			if v.existing != nil {
				currentLog := strings.Join(vsexport.ServicePortsToString(&current), ";")
				if v.existing.Service != nil {
					currentLog = "service " + v.input.PCE.Services[v.existing.Service.Href].Name
				}
				v.logChange("service_ports to be changed from %s to %s", blankLog(currentLog), strings.Join(vsexport.ServicePortsToString(&sps), ";"))
			}
			v.target.ServicePorts = &sps
		}
	}
	return nil
}

func (v *importVS) serviceAddresses() error {
	val, ok := v.cell(vsexport.HeaderServiceAddresses)
	if !ok || val == "" {
		return nil
	}
	var existing *[]ia.ServiceAddresses
	if v.existing != nil {
		existing = v.existing.ServiceAddresses
	}
	currentLog := strings.Join(vsexport.ServiceAddressesToString(existing), ";")
	if v.isRemove(val) {
		if len(ia.PtrToVal(existing)) > 0 {
			v.logChange("service_addresses to be removed (%s)", currentLog)
			v.target.ServiceAddresses = &[]ia.ServiceAddresses{}
		}
		return nil
	}
	sas, err := parseServiceAddresses(val, existing)
	if err != nil {
		return err
	}
	if serviceAddressesKey(sas) != serviceAddressesKey(ia.PtrToVal(existing)) {
		if v.existing != nil {
			v.logChange("service_addresses to be changed from %s to %s", blankLog(currentLog), strings.Join(vsexport.ServiceAddressesToString(&sas), ";"))
		}
		v.target.ServiceAddresses = &sas
	}
	return nil
}

func (v *importVS) ipOverrides() error {
	val, ok := v.cell(vsexport.HeaderIPOverrides)
	if !ok || val == "" {
		return nil
	}
	current := []string{}
	if v.existing != nil {
		current = ia.PtrToVal(v.existing.IPOverrides)
	}
	if v.isRemove(val) {
		if len(current) > 0 {
			v.logChange("ip_overrides to be removed (%s)", strings.Join(current, ";"))
			v.target.IPOverrides = &[]string{}
		}
		return nil
	}
	ips, err := parseIPOverrides(val)
	if err != nil {
		return err
	}
	if normalize(ips) != normalize(current) {
		if v.existing != nil {
			v.logChange("ip_overrides to be changed from %s to %s", blankLog(strings.Join(current, ";")), strings.Join(ips, ";"))
		}
		v.target.IPOverrides = &ips
	}
	return nil
}

func (v *importVS) applyTo() error {
	val, ok := v.cell(vsexport.HeaderApplyTo)
	if !ok || val == "" {
		return nil
	}
	if v.isRemove(val) {
		return fmt.Errorf("apply_to cannot be removed")
	}
	val = strings.ToLower(val)
	if val != "host_only" && val != "internal_bridge_network" {
		return fmt.Errorf("invalid apply_to value %s - must be host_only or internal_bridge_network", val)
	}
	if v.existing == nil || val != v.existing.ApplyTo {
		if v.existing != nil {
			v.logChange("apply_to to be changed from %s to %s", blankLog(v.existing.ApplyTo), val)
		}
		v.target.ApplyTo = val
	}
	return nil
}

func (v *importVS) externalData() error {
	targets := []*string{&v.target.ExternalDataSet, &v.target.ExternalDataReference}
	currents := []string{"", ""}
	if v.existing != nil {
		currents = []string{v.existing.ExternalDataSet, v.existing.ExternalDataReference}
	}
	for i, header := range []string{vsexport.HeaderExternalDataSet, vsexport.HeaderExternalDataReference} {
		val, ok := v.cell(header)
		if !ok || val == "" {
			continue
		}
		if v.isRemove(val) {
			utils.LogWarningf(true, "csv line %d - %s - removing %s is not supported. ignoring.", v.lineNum, v.identifier, header)
			continue
		}
		if val != currents[i] {
			if v.existing != nil {
				v.logChange("%s to be changed from %s to %s", header, blankLog(currents[i]), val)
			}
			*targets[i] = val
		}
	}
	return nil
}

// labels sets the target labels and returns the updated slice of labels that need to be created.
// The target always gets the full label set because the update sends all labels.
func (v *importVS) labels(labelKeys []string, labelCols map[string]int, newLabels []ia.Label) []ia.Label {
	pce := v.input.PCE
	final := []ia.Label{}

	// Keep existing labels for keys not in the CSV
	if v.existing != nil {
		for _, l := range ia.PtrToVal(v.existing.Labels) {
			if _, ok := labelCols[pce.Labels[l.Href].Key]; !ok {
				final = append(final, ia.Label{Href: l.Href})
			}
		}
	}

	for _, key := range labelKeys {
		val := ""
		if col := labelCols[key]; col < len(v.line) {
			val = strings.TrimSpace(v.line[col])
		}
		current := ia.Label{}
		if v.existing != nil {
			current = v.existing.GetLabelByKey(key, pce.Labels)
		}

		// Blank keeps the current label. Matching keeps the current label.
		if val == "" || val == current.Value {
			if current.Href != "" {
				final = append(final, ia.Label{Href: current.Href})
			}
			continue
		}

		// Remove value drops the label
		if v.isRemove(val) {
			if current.Href != "" {
				v.logChange("%s label of %s to be removed", key, current.Value)
			}
			continue
		}

		// New label value
		label, ok := pce.Labels[key+val]
		if !ok {
			label = ia.Label{Key: key, Value: val, Href: fmt.Sprintf("%s-%s-%s", tempLabelPrefix, key, val)}
			newLabels = append(newLabels, label)
			pce.Labels[key+val] = label
			pce.Labels[label.Href] = label
			utils.LogInfof(false, "%s label %s needs to be created", key, val)
		}
		final = append(final, ia.Label{Href: label.Href})
		if v.existing != nil {
			v.logChange("%s label to be changed from %s to %s", key, blankLog(current.Value), val)
		}
	}

	if v.existing != nil || len(final) > 0 {
		v.target.Labels = &final
	}
	return newLabels
}
