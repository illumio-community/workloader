package cloudlabel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brian1917/illumiocloudapi"
	"github.com/brian1917/workloader/utils"
)

// ImportCloudLabelsFromCSV labels existing cloud resources from a CSV.
// It never creates resources or unmanaged workloads; unmatched rows are skipped.
func ImportCloudLabelsFromCSV(input Input) {

	// Parse the CSV file
	data := input.ImportData
	var err error
	if len(data) == 0 {
		data, err = utils.ParseCSV(input.ImportFile)
		if err != nil {
			utils.LogError(err.Error())
		}
	}
	if len(data) < 2 {
		utils.LogInfo("csv has no data rows. nothing to be done", true)
		return
	}

	// Process the headers and log the input
	labelCols := input.ProcessHeaders(data[0])
	input.log()

	// Sort the label keys for deterministic processing and logging
	labelKeys := make([]string, 0, len(labelCols))
	for k := range labelCols {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	// Get the cloud resources
	objectTypes := []string{}
	if input.ObjectTypes != "" {
		for _, o := range strings.Split(input.ObjectTypes, ",") {
			if o = strings.TrimSpace(o); o != "" {
				objectTypes = append(objectTypes, o)
			}
		}
	}
	apiResps, err := input.Tenant.GetResources(illumiocloudapi.ResourcesPostRequest{ObjectType: objectTypes})
	utils.LogMultiAPIRespV2(apiResps)
	if err != nil {
		utils.LogErrorf("getting resources from cloud - %s", err)
	}
	if len(input.Tenant.Resources) == 0 {
		utils.LogInfo("no cloud resources found. nothing to be done", true)
		return
	}

	// Build the resource lookup map. Names are not guaranteed unique, so track duplicates and refuse to match on them.
	resourceMap := make(map[string]illumiocloudapi.Resource)
	duplicates := make(map[string]int)
	for _, r := range input.Tenant.Resources {
		key := r.Id
		if input.MatchString == HeaderName {
			key = r.Name
		}
		if input.IgnoreCase {
			key = strings.ToLower(key)
		}
		if key == "" {
			continue
		}
		if _, ok := resourceMap[key]; ok {
			duplicates[key]++
		}
		resourceMap[key] = r
	}

	// Iterate through CSV entries and build the label assignments
	assignments := []illumiocloudapi.LabelAssignment{}
	processed := make(map[string]int)
	notFound, noChange := 0, 0

	for i, line := range data {
		csvLine := i + 1
		if csvLine == 1 {
			continue
		}

		matchCol := input.Headers[input.MatchString]
		if matchCol >= len(line) || strings.TrimSpace(line[matchCol]) == "" {
			utils.LogWarningf(true, "csv line %d - the match column cannot be blank.", csvLine)
			continue
		}
		compareString := strings.TrimSpace(line[matchCol])
		if input.IgnoreCase {
			compareString = strings.ToLower(compareString)
		}

		// Never create - only label resources that already exist
		resource, ok := resourceMap[compareString]
		if !ok {
			notFound++
			if !input.DoNotLogEachCSVRow {
				utils.LogInfof(false, "csv line %d - %s is not an existing cloud resource. cloud-label does not create resources. nothing done.", csvLine, compareString)
			}
			continue
		}
		if duplicates[compareString] > 0 {
			utils.LogWarningf(true, "csv line %d - %s matches %d cloud resources. use resource_id to match. skipping.", csvLine, compareString, duplicates[compareString]+1)
			continue
		}
		if prev, ok := processed[resource.Id]; ok {
			utils.LogWarningf(true, "csv line %d - %s already processed on csv line %d. skipping.", csvLine, compareString, prev)
			continue
		}
		processed[resource.Id] = csvLine

		// Build the adds and removes
		add, remove := []illumiocloudapi.Label{}, []illumiocloudapi.Label{}
		for _, key := range labelKeys {
			col := labelCols[key]
			if col >= len(line) {
				continue
			}
			csvValue := strings.TrimSpace(line[col])
			currentValue := resource.GetLabelValueByKey(key)

			// Blank keeps the existing value; equal values need no change
			if csvValue == "" || csvValue == currentValue {
				continue
			}

			// Remove value clears the existing label
			if input.RemoveValue != "" && csvValue == input.RemoveValue {
				if currentValue != "" {
					remove = append(remove, illumiocloudapi.Label{Key: key, Value: currentValue})
					if !input.DoNotLogEachCSVRow {
						utils.LogInfof(false, "csv line %d - %s - %s label of %s to be removed.", csvLine, compareString, key, currentValue)
					}
				}
				continue
			}

			// New or changed value. Remove the old value if one exists.
			add = append(add, illumiocloudapi.Label{Key: key, Value: csvValue})
			logCurrent := "<empty>"
			if currentValue != "" {
				remove = append(remove, illumiocloudapi.Label{Key: key, Value: currentValue})
				logCurrent = currentValue
			}
			if !input.DoNotLogEachCSVRow {
				utils.LogInfof(false, "csv line %d - %s - %s label to be changed from %s to %s.", csvLine, compareString, key, logCurrent, csvValue)
			}
		}

		if len(add) == 0 && len(remove) == 0 {
			noChange++
			continue
		}

		a := illumiocloudapi.LabelAssignment{CspID: resource.Id}
		if len(add) > 0 {
			a.Add = &add
		}
		if len(remove) > 0 {
			a.Remove = &remove
		}
		assignments = append(assignments, a)
	}

	// Log findings
	utils.LogInfof(true, "workloader identified %d cloud resources requiring label updates.", len(assignments))
	utils.LogInfof(true, "%d csv entries require no changes.", noChange)
	utils.LogInfof(true, "%d csv entries do not match an existing cloud resource and will be skipped.", notFound)

	if len(assignments) == 0 {
		utils.LogInfo("nothing to be done", true)
		return
	}

	// If updatePCE is disabled, just log what would happen
	if !input.UpdatePCE {
		utils.LogInfo("See workloader.log for more details. To do the import, run again using --update-pce flag.", true)
		return
	}

	// Check the maximum allowed updates
	if input.MaxUpdate != -1 && len(assignments) > input.MaxUpdate {
		utils.LogErrorfCode(2, "update count for %s of %d exceeds maximum of %d. terminating run with exit code 2.", input.Tenant.FriendlyName, len(assignments), input.MaxUpdate)
	}

	// Prompt unless no-prompt is set
	if !input.NoPrompt {
		var prompt string
		fmt.Printf("\r\n%s [PROMPT] - Do you want to label %d cloud resources in %s (yes/no)? ", time.Now().Format("2006-01-02 15:04:05 "), len(assignments), input.Tenant.FriendlyName)
		fmt.Scanln(&prompt)
		if strings.ToLower(prompt) != "yes" {
			utils.LogInfo("prompt denied", true)
			return
		}
	}

	// Send the label assignments in batches
	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	failedTotal := 0
	for start := 0; start < len(assignments); start += batchSize {
		end := start + batchSize
		if end > len(assignments) {
			end = len(assignments)
		}
		batch := assignments[start:end]
		api, err := input.Tenant.LabelResources(batch)
		utils.LogAPIRespV2("LabelResources", api)

		// Log each failed resource from the response body
		resp := illumiocloudapi.LabelingPostResponse{}
		json.Unmarshal([]byte(api.RespBody), &resp)
		for _, f := range resp.FailedResources {
			utils.LogWarningf(true, "failed to label %s - %s", f.CspID, f.Error)
		}
		failedTotal += len(resp.FailedResources)

		if err != nil && len(resp.FailedResources) == 0 {
			utils.LogErrorf("labeling cloud resources - %s", err)
		}
		utils.LogInfof(true, "label assignment batch %d-%d of %d sent - status code %d - %d failed", start+1, end, len(assignments), api.StatusCode, len(resp.FailedResources))
	}

	utils.LogInfof(true, "cloud-label complete. %d resources labeled. %d failed.", len(assignments)-failedTotal, failedTotal)
}
