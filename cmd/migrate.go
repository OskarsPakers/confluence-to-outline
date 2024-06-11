/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	cf "github.com/essentialkaos/go-confluence/v6"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/spf13/cobra"
	"zzdats.lv/confluence-to-outline/confluence"
	"zzdats.lv/confluence-to-outline/outline"
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
		spaceKey, err := cmd.Flags().GetString("from")
		if err != nil {
			panic(err)
		}

		collectionId, err := cmd.Flags().GetString("to")
		if err != nil {
			panic(err)
		}

		outlineClient, err := outline.GetClient(logger)
		if err != nil {
			panic(err)
		}

		collectionInfo, err := outlineClient.Client.PostCollectionsInfoWithResponse(context.Background(), outline.PostCollectionsInfoJSONRequestBody{
			Id: uuid.MustParse(collectionId),
		})
		if err != nil {
			panic(err)
		}
		collectionTitle := *collectionInfo.JSON200.Data.Name

		confluenceClient, err := confluence.GetClient()
		if err != nil {
			panic(err)
		}

		space, err := confluenceClient.Client.GetSpace(spaceKey, cf.SpaceParameters{
			SpaceKey: []string{spaceKey},
		})
		if err != nil {
			panic(err)
		}
		markRegex, err := cmd.Flags().GetString("mark")
		if err != nil {
			panic(err)
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
			panic(err)
		}
		for _, page := range rootPages.Pages.Results {
			migrator.migratePageRecurse(page, "")
		}
		outputDataToJSON(migrator.urlMap, "urlMap")
		migrator.fixURLs()

	},
}

func (m Migrator) fixURLs() {

	var checkURLs, checkStringJSON []JsonOutputVars
	for _, urlInfo := range m.urlMap {
		resp, err := m.outlineClient.Client.PostDocumentsInfoWithResponse(context.Background(), outline.PostDocumentsInfoJSONRequestBody{
			Id: &urlInfo.DocId,
		})
		if err != nil {
			panic(err)
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

func (m Migrator) importDocumentExportedFromOutline(page *cf.Content, parentDocumentId string, exportedDoc *string) *outline.PostDocumentsImportResponse {
	var publish = true

	exportedDocBytes, err := os.ReadFile("tmp/" + *exportedDoc)
	if err != nil {
		panic(err)
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
		panic(err)
	}
	return importDocumentRes
}

func (m Migrator) migratePageRecurse(page *cf.Content, parentDocumentId string) {
	exportedDoc, err := m.confluenceClient.ExportDoc(page.ID)
	if err != nil {
		panic(err)
	}
	importDocumentRes := m.importDocumentExportedFromOutline(page, parentDocumentId, exportedDoc)
	m.createPageMapping(page, importDocumentRes)

	os.Remove("tmp/" + *exportedDoc)
	createdDocumentId := *importDocumentRes.JSON200.Data.Id
	m.logger.Info("Imported document", "documentId", createdDocumentId, "documentTitle", *importDocumentRes.JSON200.Data.Title)

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return
	}
	m.logger.Info("Migrating child pages", "childPageCount", page.Children.Pages.Size, "pageId", page.ID, "pageTitle", page.Title)

	for _, childPage := range page.Children.Pages.Results {

		childPageFull, err := m.confluenceClient.Client.GetContentByID(childPage.ID, cf.ContentIDParameters{
			Expand: []string{"version", "body.storage", "children.page"},
		})
		if err != nil {
			panic(err)
		}
		m.migratePageRecurse(childPageFull, createdDocumentId.String())
	}
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
		panic(err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\n")

	err = encoder.Encode(data)
	if err != nil {
		panic(err)
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
