package unusedumwl

import (
	"fmt"
	"strings"

	"github.com/brian1917/illumioapi"
	"github.com/brian1917/workloader/utils"
)

// getTargetUmwls returns the unmanaged workloads to check based on the label-file, href-file, or all unmanaged workloads.
func getTargetUmwls() []illumioapi.Workload {

	if labelFile != "" && hrefFile != "" {
		utils.LogError("cannot provide both a label-file and href-file.")
	}

	if hrefFile != "" {
		return getUmwlsFromHrefFile()
	}

	qp := map[string]string{"managed": "false"}

	if labelFile != "" {
		labelCsvData, err := utils.ParseCSV(labelFile)
		if err != nil {
			utils.LogError(fmt.Sprintf("parsing label-file - %s", err))
		}
		if len(labelCsvData) < 2 {
			utils.LogError("label-file must have a header row and at least one row of labels.")
		}
		labelQuery, err := pce.WorkloadQueryLabelParameter(labelCsvData)
		if err != nil {
			utils.LogError(fmt.Sprintf("getting label parameter query - %s", err))
		}
		if len(labelQuery) > 10000 {
			utils.LogError(fmt.Sprintf("the query is too large. the total character count is %d and the limit for this command is 10,000", len(labelQuery)))
		}
		qp["labels"] = labelQuery
	}

	umwls, a, err := pce.GetWklds(qp)
	utils.LogAPIResp("GetWklds", a)
	if err != nil {
		utils.LogError(err.Error())
	}

	return umwls
}

// getUmwlsFromHrefFile gets each workload in the href file and returns the unmanaged ones.
func getUmwlsFromHrefFile() []illumioapi.Workload {
	csvData, err := utils.ParseCSV(hrefFile)
	if err != nil {
		utils.LogError(fmt.Sprintf("parsing href-file - %s", err))
	}

	// Use the "href" column if a header exists; otherwise the first column.
	hrefCol := 0
	if len(csvData) > 0 {
		for i, h := range csvData[0] {
			if strings.EqualFold(strings.TrimSpace(h), "href") {
				hrefCol = i
				break
			}
		}
	}

	umwls := []illumioapi.Workload{}
	seen := make(map[string]bool)
	for i, row := range csvData {
		if len(row) <= hrefCol {
			continue
		}
		href := strings.TrimSpace(row[hrefCol])

		// Skip header rows and blanks
		if href == "" || !strings.Contains(href, "/workloads/") {
			if i != 0 {
				utils.LogWarning(fmt.Sprintf("href-file line %d - %s is not a workload href. skipping.", i+1, href), true)
			}
			continue
		}
		if seen[href] {
			continue
		}
		seen[href] = true

		wkld, a, err := pce.GetWkldByHref(href)
		utils.LogAPIResp("GetWkldByHref", a)
		if err != nil {
			utils.LogWarning(fmt.Sprintf("href-file line %d - getting %s - %s. skipping.", i+1, href, err), true)
			continue
		}
		if wkld.GetMode() != "unmanaged" {
			utils.LogWarning(fmt.Sprintf("href-file line %d - %s is a managed workload. skipping.", i+1, href), true)
			continue
		}
		umwls = append(umwls, wkld)
	}

	return umwls
}
