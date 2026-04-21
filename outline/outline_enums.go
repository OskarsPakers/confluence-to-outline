package outline

// Hand-written companions to outline.gen.go.
//
// oapi-codegen v2 with compatibility.old-merge-schemas=true (required because
// the spec has allOf blocks with mismatched `nullable` that the new merge
// path refuses) does not emit the inline enum types defined inside those
// allOf blocks — it only emits the outer struct that references them.
//
// These 4 types are the ones referenced-but-not-defined in the generated
// file. If you regenerate and the list of missing types changes, update
// this file accordingly.

// PostDocumentsDraftsJSONBodyDateFilter defines parameters for PostDocumentsDrafts.
type PostDocumentsDraftsJSONBodyDateFilter string

const (
	PostDocumentsDraftsJSONBodyDateFilterDay   PostDocumentsDraftsJSONBodyDateFilter = "day"
	PostDocumentsDraftsJSONBodyDateFilterWeek  PostDocumentsDraftsJSONBodyDateFilter = "week"
	PostDocumentsDraftsJSONBodyDateFilterMonth PostDocumentsDraftsJSONBodyDateFilter = "month"
	PostDocumentsDraftsJSONBodyDateFilterYear  PostDocumentsDraftsJSONBodyDateFilter = "year"
)

// PostDocumentsSearchJSONBodyDateFilter defines parameters for PostDocumentsSearch.
type PostDocumentsSearchJSONBodyDateFilter string

const (
	PostDocumentsSearchJSONBodyDateFilterDay   PostDocumentsSearchJSONBodyDateFilter = "day"
	PostDocumentsSearchJSONBodyDateFilterWeek  PostDocumentsSearchJSONBodyDateFilter = "week"
	PostDocumentsSearchJSONBodyDateFilterMonth PostDocumentsSearchJSONBodyDateFilter = "month"
	PostDocumentsSearchJSONBodyDateFilterYear  PostDocumentsSearchJSONBodyDateFilter = "year"
)

// PostFileOperationsListJSONBodyType defines parameters for PostFileOperationsList.
type PostFileOperationsListJSONBodyType string

const (
	PostFileOperationsListJSONBodyTypeExport PostFileOperationsListJSONBodyType = "export"
	PostFileOperationsListJSONBodyTypeImport PostFileOperationsListJSONBodyType = "import"
)

// PostUsersListJSONBodyFilter defines parameters for PostUsersList.
type PostUsersListJSONBodyFilter string

const (
	PostUsersListJSONBodyFilterInvited   PostUsersListJSONBodyFilter = "invited"
	PostUsersListJSONBodyFilterViewers   PostUsersListJSONBodyFilter = "viewers"
	PostUsersListJSONBodyFilterAdmins    PostUsersListJSONBodyFilter = "admins"
	PostUsersListJSONBodyFilterActive    PostUsersListJSONBodyFilter = "active"
	PostUsersListJSONBodyFilterAll       PostUsersListJSONBodyFilter = "all"
	PostUsersListJSONBodyFilterSuspended PostUsersListJSONBodyFilter = "suspended"
)
