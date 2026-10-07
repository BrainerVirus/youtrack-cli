package cmdtest

import "encoding/json"

// sampleIssues are two issues as YouTrack returns them with every attribute
// requested, including custom fields of many $types and one $type ytrack
// does not know.
const sampleIssues = `[
  {
    "$type": "Issue",
    "id": "2-4040",
    "idReadable": "NSR-40",
    "summary": "Login redirect drops the return URL",
    "description": "After SSO login the user lands on the dashboard.\r\n\r\nExpected: back to the page they came from.",
    "project": {"$type": "Project", "id": "0-12", "shortName": "NSR", "name": "Nightshift"},
    "reporter": {"$type": "User", "id": "1-7", "login": "jroe", "fullName": "Jane Roe"},
    "created": 1790000000000,
    "updated": 1791300000000,
    "resolved": null,
    "tags": [{"$type": "IssueTag", "id": "6-1", "name": "backend"}, {"$type": "IssueTag", "id": "6-2", "name": "sso"}],
    "customFields": [
      {"$type": "SingleEnumIssueCustomField", "id": "92-1", "name": "Priority", "value": {"$type": "EnumBundleElement", "id": "67-1", "name": "Major"}},
      {"$type": "SingleEnumIssueCustomField", "id": "92-2", "name": "Type", "value": {"$type": "EnumBundleElement", "id": "67-9", "name": "Bug"}},
      {"$type": "StateIssueCustomField", "id": "92-3", "name": "State", "value": {"$type": "StateBundleElement", "id": "69-2", "name": "In Progress", "isResolved": false}},
      {"$type": "SingleUserIssueCustomField", "id": "92-4", "name": "Assignee", "value": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"}},
      {"$type": "MultiOwnedIssueCustomField", "id": "92-5", "name": "Subsystem", "value": [{"$type": "OwnedBundleElement", "id": "70-1", "name": "Auth"}, {"$type": "OwnedBundleElement", "id": "70-2", "name": "Web"}]},
      {"$type": "MultiVersionIssueCustomField", "id": "92-6", "name": "Fix versions", "value": [{"$type": "VersionBundleElement", "id": "71-1", "name": "2026.3"}]},
      {"$type": "PeriodIssueCustomField", "id": "92-7", "name": "Estimation", "value": {"$type": "PeriodValue", "id": "p-1", "minutes": 750, "presentation": "1d 4h 30m"}},
      {"$type": "PeriodIssueCustomField", "id": "92-8", "name": "Spent time", "value": null},
      {"$type": "DateIssueCustomField", "id": "92-9", "name": "Due Date", "value": 1791374400000},
      {"$type": "SimpleIssueCustomField", "id": "92-10", "name": "Story points", "value": 5},
      {"$type": "SimpleIssueCustomField", "id": "92-13", "name": "Deployed", "projectCustomField": {"$type": "SimpleProjectCustomField", "field": {"$type": "CustomField", "fieldType": {"$type": "FieldType", "id": "date and time"}}}, "value": 1791374400000},
      {"$type": "TextIssueCustomField", "id": "92-11", "name": "Root cause", "value": {"$type": "TextFieldValue", "id": "t-1", "text": "Session cookie set before redirect"}},
      {"$type": "HologramIssueCustomField", "id": "92-12", "name": "Sentiment", "value": {"$type": "Mood", "score": 7}}
    ]
  },
  {
    "$type": "Issue",
    "id": "2-4041",
    "idReadable": "NSR-41",
    "summary": "Dark mode toggle",
    "description": null,
    "project": {"$type": "Project", "id": "0-12", "shortName": "NSR", "name": "Nightshift"},
    "reporter": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"},
    "created": 1791200000000,
    "updated": 1791350000000,
    "resolved": 1791350000000,
    "tags": [],
    "customFields": [
      {"$type": "SingleEnumIssueCustomField", "id": "92-1", "name": "Priority", "value": {"$type": "EnumBundleElement", "id": "67-3", "name": "Normal"}},
      {"$type": "StateIssueCustomField", "id": "92-3", "name": "State", "value": {"$type": "StateBundleElement", "id": "69-4", "name": "Fixed", "isResolved": true}},
      {"$type": "SingleUserIssueCustomField", "id": "92-4", "name": "Assignee", "value": null}
    ]
  }
]`

// sampleComments are NSR-40's comments, oldest first.
const sampleComments = `[
  {"$type": "IssueComment", "id": "4-1", "text": "Reproduced on staging.", "author": {"$type": "User", "login": "jroe", "fullName": "Jane Roe"}, "created": 1791200000000, "updated": null},
  {"$type": "IssueComment", "id": "4-2", "text": "The return URL is lost\nin the SAML relay state.", "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "created": 1791300000000, "updated": 1791350000000},
  {"$type": "IssueComment", "id": "4-3", "text": "Fix is up for review.", "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "created": 1791360000000, "updated": null}
]`

// SeedSampleIssues replaces Issues with NSR-40 and NSR-41 and gives NSR-40
// three comments.
func (yt *FakeYouTrack) SeedSampleIssues() {
	var issues, comments []map[string]any
	if err := json.Unmarshal([]byte(sampleIssues), &issues); err != nil {
		panic(err)
	}
	if err := json.Unmarshal([]byte(sampleComments), &comments); err != nil {
		panic(err)
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	yt.Issues = issues
	yt.Comments = map[string][]map[string]any{"NSR-40": comments}
}

// sampleWorkItems are NSR-40's work items as YouTrack returns them with
// every attribute, by two authors, with and without a type.
const sampleWorkItems = `[
  {"$type": "IssueWorkItem", "id": "115-1", "date": 1790726400000, "duration": {"$type": "DurationValue", "id": "d-1", "minutes": 90, "presentation": "1h 30m"}, "author": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"}, "creator": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"}, "type": {"$type": "WorkItemType", "id": "117-0", "name": "Development", "autoAttached": true}, "text": "Traced the lost return URL", "textPreview": "<p>Traced the lost return URL</p>", "created": 1790760000000, "updated": 1790760000000, "issue": {"$type": "Issue", "id": "2-4040", "idReadable": "NSR-40"}, "attributes": []},
  {"$type": "IssueWorkItem", "id": "115-2", "date": 1790812800000, "duration": {"$type": "DurationValue", "id": "d-2", "minutes": 30, "presentation": "30m"}, "author": {"$type": "User", "id": "1-7", "login": "jroe", "fullName": "Jane Roe"}, "creator": {"$type": "User", "id": "1-7", "login": "jroe", "fullName": "Jane Roe"}, "type": null, "text": "Meetings", "textPreview": "<p>Meetings</p>", "created": 1790850000000, "updated": 1790850000000, "issue": {"$type": "Issue", "id": "2-4040", "idReadable": "NSR-40"}, "attributes": []},
  {"$type": "IssueWorkItem", "id": "115-3", "date": 1791158400000, "duration": {"$type": "DurationValue", "id": "d-3", "minutes": 480, "presentation": "1d"}, "author": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"}, "creator": {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"}, "type": {"$type": "WorkItemType", "id": "117-1", "name": "Testing", "autoAttached": false}, "text": "", "textPreview": "", "created": 1791190000000, "updated": 1791200000000, "issue": {"$type": "Issue", "id": "2-4040", "idReadable": "NSR-40"}, "attributes": []}
]`

// sampleTimeTracking is the Nightshift project's time tracking settings.
const sampleTimeTracking = `{
  "$type": "ProjectTimeTrackingSettings", "id": "0-12", "enabled": true,
  "estimate": {"$type": "PeriodProjectCustomField", "id": "93-1"},
  "timeSpent": {"$type": "PeriodProjectCustomField", "id": "93-2"},
  "workItemTypes": [
    {"$type": "WorkItemType", "id": "117-0", "name": "Development", "autoAttached": true},
    {"$type": "WorkItemType", "id": "117-1", "name": "Testing", "autoAttached": false},
    {"$type": "WorkItemType", "id": "117-2", "name": "Documentation", "autoAttached": false}
  ]
}`

// SeedSampleWorkItems gives NSR-40 three work items and the NSR project
// (0-12) time tracking with the types Development, Testing and
// Documentation. Seed the issues first.
func (yt *FakeYouTrack) SeedSampleWorkItems() {
	var items []map[string]any
	var settings map[string]any
	if err := json.Unmarshal([]byte(sampleWorkItems), &items); err != nil {
		panic(err)
	}
	if err := json.Unmarshal([]byte(sampleTimeTracking), &settings); err != nil {
		panic(err)
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	yt.WorkItems = map[string][]map[string]any{"NSR-40": items}
	yt.TimeTracking = map[string]map[string]any{"0-12": settings}
}
