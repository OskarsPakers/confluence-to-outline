package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"oskarspakers/confluence-to-outline/confluence"
	"oskarspakers/confluence-to-outline/outline"

	cf "github.com/essentialkaos/go-confluence/v6"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/spf13/cobra"
)

type UrlMapEntry struct {
	NewUrl string
	DocId  string
}

type JsonOutputVars struct {
	Counter int
	Id      string `json:"DocumentID"`
	ConfURL string `json:"ConfluenceURL"`
	OutUrl  string `json:"OutlineURL"`
}

type DocumentData struct {
	DocId   string
	DocBody string
	Title   string
}

type Migrator struct {
	confluenceClient *confluence.ConfluenceExtendedClient
	outlineClient    *outline.OutlineExtendedClient
	urlMap           map[string]UrlMapEntry
	spaceKey         string
	collectionId     string
	markRegex        string
	logger           *slog.Logger
}

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate confluence pages to outline documents",
	Long:  `Exports word documents from confluence space and uploads them to outline collection.`,
	Run: func(cmd *cobra.Command, args []string) {
		lvl := new(slog.LevelVar)
		levelString := cmd.Flag("log").Value.String()
		lvl.UnmarshalText([]byte(levelString))
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: lvl,
		}))

		fatal := func(msg string, err error) {
			if err != nil {
				logger.Error(msg, "error", err)
			} else {
				logger.Error(msg)
			}
			os.Exit(1)
		}

		spaceKey, err := cmd.Flags().GetString("from")
		if err != nil {
			fatal("Error getting --from flag", err)
		}

		collectionId, err := cmd.Flags().GetString("to")
		if err != nil {
			fatal("Error getting --to flag", err)
		}

		rateLimit, err := outlineRateLimitFromFlags(cmd)
		if err != nil {
			fatal(err.Error(), nil)
		}

		outlineClient, err := outline.GetClient(logger, rateLimit)
		if err != nil {
			fatal("Error creating Outline client", err)
		}

		collectionInfo, err := outlineClient.Client.PostCollectionsInfoWithResponse(context.Background(), outline.PostCollectionsInfoJSONRequestBody{
			Id: uuid.MustParse(collectionId),
		})
		if err != nil {
			fatal("Error getting Outline collection info", err)
		}
		if collectionInfo.JSON200 == nil {
			fatal(fmt.Sprintf("failed to get Outline collection (status %d): %s", collectionInfo.StatusCode(), string(collectionInfo.Body)), nil)
		}
		collectionTitle := *collectionInfo.JSON200.Data.Name

		confluenceClient, err := confluence.GetClient()
		if err != nil {
			fatal("Error creating Confluence client", err)
		}

		space, err := confluenceClient.Client.GetSpace(spaceKey, cf.SpaceParameters{
			SpaceKey: []string{spaceKey},
		})
		if err != nil {
			fatal("Error getting Confluence space", err)
		}

		markRegex, err := cmd.Flags().GetString("mark")
		if err != nil {
			fatal("Error getting --mark flag", err)
		}

		migrator := Migrator{
			confluenceClient: confluenceClient,
			outlineClient:    outlineClient,
			urlMap:           make(map[string]UrlMapEntry),
			spaceKey:         spaceKey,
			collectionId:     collectionId,
			markRegex:        markRegex,
			logger:           logger,
		}

		logger.Info("Migrating confluence pages to Outline collection", "spaceKey", spaceKey, "spaceName", space.Name, "collectionId", collectionId, "collectionTitle", collectionTitle)

		rootPages, err := confluenceClient.Client.GetSpaceContent(spaceKey, cf.SpaceParameters{
			SpaceKey: []string{spaceKey},
			Expand:   []string{"version", "body.storage", "children.page"},
			Depth:    "1",
			Limit:    10,
		})
		if err != nil {
			fatal("Error getting Confluence space content", err)
		}
		for _, page := range rootPages.Pages.Results {
			if err := migrator.migratePageRecurse(page, ""); err != nil {
				fatal("Migration failed", err)
			}
		}
		outputDataToJSON(migrator.urlMap, "urlMap")
		migrator.fixURLs()

		if err := os.RemoveAll("export"); err != nil {
			logger.Warn("Failed to remove export folder", "error", err)
		} else {
			logger.Info("Removed export folder")
		}

	},
}

func (m Migrator) fixURLs() {

	var checkURLs, checkStringJSON []JsonOutputVars
	for _, urlInfo := range m.urlMap {
		resp, err := m.outlineClient.Client.PostDocumentsInfoWithResponse(context.Background(), outline.PostDocumentsInfoJSONRequestBody{
			Id: &urlInfo.DocId,
		})
		if err != nil {
			m.logger.Error("Error getting document info", "error", err)
			continue
		}
		document := resp.JSON200
		documentData := DocumentData{DocId: (*document.Data.Id).String(), DocBody: *document.Data.Text, Title: *document.Data.Title}
		for oldUrl, urlInfo := range m.urlMap {
			documentData.DocBody = m.replaceUrlInDocument(oldUrl, urlInfo, documentData.DocBody)
			checkURLs = m.markBrokenLinks(urlInfo, documentData, checkURLs)
		}
		if m.markRegex != "" {
			checkStringJSON = m.markRegexFunc(documentData, checkStringJSON)
		}
		m.updateOutlineDocument(documentData)
	}
	outputMarkedPages(checkURLs, "checkURLs")
	outputMarkedPages(checkStringJSON, "Marked")
}

func outputMarkedPages(data []JsonOutputVars, filename string) {
	for i := range data {
		data[i].Counter = i + 1
	}
	outputDataToJSON(data, filename)
}

func (m Migrator) replaceUrlInDocument(oldUrl string, urlMapEntry UrlMapEntry, documentBody string) string {
	confluenceHostname := strings.TrimSuffix(m.confluenceClient.GetBaseURL(), "/")
	outlineHostname := strings.TrimSuffix(m.outlineClient.GetBaseURL(), "/api")

	documentBody = strings.ReplaceAll(documentBody, "("+oldUrl+")", "("+urlMapEntry.NewUrl+")")

	oldUrlAbsolute := confluenceHostname + oldUrl
	newUrlAbsolute := outlineHostname + urlMapEntry.NewUrl
	documentBody = strings.ReplaceAll(documentBody, "("+oldUrlAbsolute+")", "("+newUrlAbsolute+")")

	return documentBody
}

func (m Migrator) markBrokenLinks(urlMapEntry UrlMapEntry, documentData DocumentData, markedURLs []JsonOutputVars) []JsonOutputVars {
	outlineHostname := strings.TrimSuffix(m.outlineClient.GetBaseURL(), "/api")
	newUrl := urlMapEntry.NewUrl
	newUrlAbsolute := outlineHostname + urlMapEntry.NewUrl
	if strings.Contains(documentData.DocBody, "\n]("+newUrlAbsolute+")[") ||
		strings.Contains(documentData.DocBody, "\n]("+newUrl+")[") {
		markedURLs = m.addToMarkedList(documentData, markedURLs)
	}
	return markedURLs
}

func (m Migrator) addToMarkedList(documentData DocumentData, markedURLs []JsonOutputVars) []JsonOutputVars {
	for oldUrl, urlInfoFromMap := range m.urlMap {
		if urlInfoFromMap.DocId == documentData.DocId {
			for _, docInfo := range markedURLs {
				if docInfo.Id == urlInfoFromMap.DocId {
					return markedURLs
				}
			}
			return append(markedURLs, JsonOutputVars{Id: urlInfoFromMap.DocId, OutUrl: urlInfoFromMap.NewUrl, ConfURL: oldUrl})
		}
	}
	return markedURLs
}

func (m Migrator) markRegexFunc(documentData DocumentData, checkStringJSON []JsonOutputVars) []JsonOutputVars {
	if (m.markRegex != "") && regexp.MustCompile(m.markRegex).MatchString(documentData.DocBody) {
		checkStringJSON = m.addToMarkedList(documentData, checkStringJSON)
	}
	return checkStringJSON
}

func (m Migrator) updateOutlineDocument(documentData DocumentData) error {
	publish := true // Document update vars https://www.getoutline.com/developers#tag/Documents/paths/~1documents.update/post
	appendDoc := false
	done := true
	_, err := m.outlineClient.Client.PostDocumentsUpdateWithResponse(context.Background(), outline.PostDocumentsUpdateJSONRequestBody{
		Id:      documentData.DocId,
		Title:   &documentData.Title,
		Text:    &documentData.DocBody,
		Append:  &appendDoc,
		Publish: &publish,
		Done:    &done,
	})
	return err
}

func (m Migrator) importDocumentExportedFromOutline(page *cf.Content, parentDocumentId string, exportedDoc *string) (*outline.PostDocumentsImportResponse, error) {
	var publish = true

	exportedDocBytes, err := os.ReadFile("export/" + *exportedDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to read exported file for page %s (%s): %w", page.ID, page.Title, err)
	}

	collectionUuid := uuid.MustParse(m.collectionId)
	importFileRequest := map[string]any{
		"file": exportedDocBytes,
	}
	importDocumentReq := outline.PostDocumentsImportMultipartRequestBody{
		CollectionId: &collectionUuid,

		// File Only plain text, markdown, docx, and html format are supported.
		File:    &importFileRequest,
		Publish: &publish,
	}
	if parentDocumentId != "" {
		parentDocumentUuid := uuid.MustParse(parentDocumentId)
		importDocumentReq.ParentDocumentId = &parentDocumentUuid
	}
	importDocumentRes, err := m.outlineClient.ImportDocument(importDocumentReq, *exportedDoc, page.Title)
	if err != nil {
		return nil, fmt.Errorf("ImportDocument failed for page %s (%s): %w", page.ID, page.Title, err)
	}
	return importDocumentRes, nil
}

func (m Migrator) processImagesInHTMLFile(filename string) error {
	content, err := os.ReadFile("export/" + filename)
	if err != nil {
		return err
	}

	htmlContent := string(content)
	confluenceBase := strings.TrimSuffix(m.confluenceClient.GetBaseURL(), "/")

	// For resolving absolute paths (starting with "/"), use only the scheme+host
	// to avoid double-path like /wiki/wiki/... when confluenceBase already contains a path.
	confluenceOrigin := confluenceBase
	if u, err := url.Parse(confluenceBase); err == nil {
		confluenceOrigin = u.Scheme + "://" + u.Host
	}

	// Match all <img src="..."> occurrences
	imgSrcRegex := regexp.MustCompile(`(<img[^>]+src=")([^"]+)(")`)

	var processErr error
	newContent := imgSrcRegex.ReplaceAllStringFunc(htmlContent, func(match string) string {
		parts := imgSrcRegex.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		imgSrc := parts[2]

		// Resolve to absolute URL; skip external images that aren't from Confluence or Atlassian media
		var imgURL string
		if strings.HasPrefix(imgSrc, "http://") || strings.HasPrefix(imgSrc, "https://") {
			isConfluence := strings.HasPrefix(imgSrc, confluenceBase)
			isAtlassianMedia := strings.HasPrefix(imgSrc, "https://api.media.atlassian.com/")
			if !isConfluence && !isAtlassianMedia {
				return match
			}
			imgURL = imgSrc
		} else if strings.HasPrefix(imgSrc, "/") {
			imgURL = confluenceOrigin + imgSrc
		} else {
			return match
		}

		// Strip query params for the filename
		imgFilename := path.Base(strings.SplitN(imgSrc, "?", 2)[0])
		if imgFilename == "" || imgFilename == "." {
			imgFilename = "image.png"
		}

		imageData, contentType, err := m.confluenceClient.DownloadImage(imgURL)
		if err != nil {
			m.logger.Warn("Failed to download image", "url", imgURL, "error", err)
			return match
		}

		// Save image to export/images for inspection
		_ = os.MkdirAll("export/images", 0755)
		_ = os.WriteFile("export/images/"+imgFilename, imageData, 0644)

		outlineURL, err := m.outlineClient.UploadAttachment(imageData, imgFilename, contentType)
		if err != nil {
			m.logger.Warn("Failed to upload image to Outline", "url", imgURL, "error", err)
			return match
		}

		return parts[1] + outlineURL + parts[3]
	})

	if processErr != nil {
		return processErr
	}

	return os.WriteFile("export/"+filename, []byte(newContent), 0644)
}

func processCodeBlocksInHTMLFile(filename string) error {
	content, err := os.ReadFile("export/" + filename)
	if err != nil {
		return err
	}

	html := string(content)

	// Match Confluence code panel: <div class="code panel...">...<pre class="syntaxhighlighter-pre" data-syntaxhighlighter-params="brush: LANG; ...">CODE</pre>...</div></div>
	codePanelRegex := regexp.MustCompile(`(?s)<div[^>]+class="[^"]*code panel[^"]*"[^>]*>.*?<pre[^>]*data-syntaxhighlighter-params="brush:\s*([^;"\s]+)[^"]*"[^>]*>(.*?)</pre>.*?</div>\s*</div>`)
	brushLangRegex := regexp.MustCompile(`brush:\s*([^;"\s]+)`)

	html = codePanelRegex.ReplaceAllStringFunc(html, func(match string) string {
		lang := ""
		if m := brushLangRegex.FindStringSubmatch(match); len(m) > 1 {
			lang = m[1]
		}
		// Extract code content between <pre ...> and </pre>
		preRegex := regexp.MustCompile(`(?s)<pre[^>]*>(.*?)</pre>`)
		preMatch := preRegex.FindStringSubmatch(match)
		if len(preMatch) < 2 {
			return match
		}
		code := preMatch[1]
		if lang != "" {
			return `<pre><code class="language-` + lang + `">` + code + `</code></pre>`
		}
		return `<pre><code>` + code + `</code></pre>`
	})

	return os.WriteFile("export/"+filename, []byte(html), 0644)
}

func (m Migrator) migratePageRecurse(page *cf.Content, parentDocumentId string) error {
	exportedDoc, err := m.confluenceClient.ExportDoc(page.ID)
	if err != nil {
		return fmt.Errorf("failed to export page %s (%s): %w", page.ID, page.Title, err)
	}
	if err := m.processImagesInHTMLFile(*exportedDoc); err != nil {
		m.logger.Warn("Failed to process images", "pageId", page.ID, "pageTitle", page.Title, "error", err)
	}
	if err := processCodeBlocksInHTMLFile(*exportedDoc); err != nil {
		m.logger.Warn("Failed to process code blocks", "pageId", page.ID, "pageTitle", page.Title, "error", err)
	}
	importDocumentRes, err := m.importDocumentExportedFromOutline(page, parentDocumentId, exportedDoc)
	if err != nil {
		return err
	}
	if importDocumentRes.JSON200 == nil {
		return fmt.Errorf("import failed for page %s (%s): status %d body: %s", page.ID, page.Title, importDocumentRes.StatusCode(), string(importDocumentRes.Body))
	}
	m.createPageMapping(page, importDocumentRes)

	createdDocumentId := *importDocumentRes.JSON200.Data.Id
	m.logger.Info("Imported document", "documentId", createdDocumentId, "documentTitle", *importDocumentRes.JSON200.Data.Title)

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return nil
	}
	m.logger.Info("Migrating child pages", "childPageCount", page.Children.Pages.Size, "pageId", page.ID, "pageTitle", page.Title)

	for _, childPage := range page.Children.Pages.Results {

		childPageFull, err := m.confluenceClient.Client.GetContentByID(childPage.ID, cf.ContentIDParameters{
			Expand: []string{"version", "body.storage", "children.page"},
		})
		if err != nil {
			return fmt.Errorf("failed to get child page %s: %w", childPage.ID, err)
		}
		if err := m.migratePageRecurse(childPageFull, createdDocumentId.String()); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) createPageMapping(page *cf.Content, importDocumentRes *outline.PostDocumentsImportResponse) {
	createdDocumentId := *importDocumentRes.JSON200.Data.Id
	title := *importDocumentRes.JSON200.Data.Title
	urlId := *importDocumentRes.JSON200.Data.UrlId
	titleSlug := slug.Make(title) // Slug is not present for input document response
	destOutlineUrl := fmt.Sprintf(`/doc/%s-%s`, titleSlug, urlId)
	confluenceURLs := m.getPossibleConfluenceURLs(page)
	for i := range confluenceURLs {
		m.urlMap = updateUrlMap(m.urlMap, confluenceURLs[i], destOutlineUrl, createdDocumentId.String())
	}
}

func (m Migrator) getPossibleConfluenceURLs(page *cf.Content) []string {
	var urls []string
	urls = append(urls, fmt.Sprintf(`/pages/viewpage.action?pageId=%s`, page.ID))
	encodedTitle := strings.ReplaceAll(page.Title, ":", "%3A")
	urls = append(urls, fmt.Sprintf(`/display/%s/%s`, m.spaceKey, encodedTitle))
	encodedTitle = strings.ReplaceAll(encodedTitle, " ", "+")
	urls = append(urls, fmt.Sprintf(`/display/%s/%s`, m.spaceKey, encodedTitle))
	return urls
}

func updateUrlMap(urlMap map[string]UrlMapEntry, oldUrl, newUrl, createdDocumentId string) map[string]UrlMapEntry {
	urlMap[oldUrl] = UrlMapEntry{NewUrl: newUrl, DocId: createdDocumentId}
	return urlMap
}

func outputDataToJSON(data interface{}, filename string) {
	file, err := os.Create(filename + ".json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating %s.json: %v\n", filename, err)
		os.Exit(1)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\n")

	if err = encoder.Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON to %s.json: %v\n", filename, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.PersistentFlags().String("from", "", "Confluence SpaceKey to migrate pages from")
	migrateCmd.MarkPersistentFlagRequired("from")
	migrateCmd.PersistentFlags().String("to", "", "Outline collection id to import documents into")
	migrateCmd.MarkPersistentFlagRequired("to")
	migrateCmd.PersistentFlags().String("mark", "", "Regex pattern within pages to review later. List of pages matching regex are saved in a Marked.json file for manual review.")

}
