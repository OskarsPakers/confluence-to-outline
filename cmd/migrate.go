/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
}

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate confluence pages to outline documents",
	Long:  `Exports word documents from confluence space and uploads them to outline collection.`,
	Run: func(cmd *cobra.Command, args []string) {
		spaceKey, err := cmd.Flags().GetString("from")
		if err != nil {
			panic(err)
		}

		collectionId, err := cmd.Flags().GetString("to")
		if err != nil {
			panic(err)
		}

		outlineClient, err := outline.GetClient()
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
		}

		fmt.Printf("Migrating confluence pages from Confluence space \"%s\" (%s) to Outline collection \"%s\" (%s).\n", spaceKey, space.Name, collectionId, collectionTitle)

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
		migrator.saveUrlMapToFile() //Comment out to disable saving URL map to json file
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
		documentBody := *document.Data.Text
		documentId := *document.Data.Id
		documentData := DocumentData{DocId: documentId.String(), DocBody: documentBody, Title: *document.Data.Title}
		for oldUrl, urlInfo := range m.urlMap {
			documentData.DocBody = m.replaceUrlInDocument(oldUrl, urlInfo, documentData.DocBody)
			checkURLs = m.markBrokenLinks(urlInfo, documentData, checkURLs)
		}
		if m.markRegex != "" {
			checkStringJSON = m.markRegexFunc(documentData, checkStringJSON)
		}
		m.updateOutlineDocument(documentData)
	}
	outputJSON(checkURLs, "checkURLs")    //Comment out if you dont want json debug output files
	outputJSON(checkStringJSON, "Marked") //Comment out if you dont want json debug output files
}

func (m Migrator) replaceUrlInDocument(oldUrl string, urlMapEntry UrlMapEntry, documentBody string) string {
	confluenceHostname := strings.TrimSuffix(m.confluenceClient.GetBaseURL(), "/")
	outlineHostname := strings.TrimSuffix(m.outlineClient.GetBaseURL(), "/api")

	oldUrlWrapped := "(" + oldUrl + ")" //Relative URL
	newUrlWrapped := "(" + urlMapEntry.NewUrl + ")"
	oldUrlHostnameWrapped := "(" + confluenceHostname + oldUrl + ")" //Absolute URL
	newUrlHostnameWrapped := "(" + outlineHostname + urlMapEntry.NewUrl + ")"
	documentBody = strings.ReplaceAll(documentBody, oldUrlWrapped, newUrlWrapped)
	documentBody = strings.ReplaceAll(documentBody, oldUrlHostnameWrapped, newUrlHostnameWrapped)
	return documentBody
}

func (m Migrator) markBrokenLinks(urlMapEntry UrlMapEntry, documentData DocumentData, checkURLs []JsonOutputVars) []JsonOutputVars {
	outlineHostname := strings.TrimSuffix(m.outlineClient.GetBaseURL(), "/api")
	newUrlWrapped := "(" + urlMapEntry.NewUrl + ")"
	newUrlHostnameWrapped := "(" + outlineHostname + urlMapEntry.NewUrl + ")"
	if strings.Contains(documentData.DocBody, "\n]"+newUrlHostnameWrapped+"[") || strings.Contains(documentData.DocBody, "\n]"+newUrlWrapped+"[") {
		for oldUrl, urlInfoFromMap := range m.urlMap { //Getting the old confluence URL with only knowing document Id
			if urlInfoFromMap.DocId == documentData.DocId {
				exists := false
				for _, docInfo := range checkURLs {
					if docInfo.Id == urlInfoFromMap.DocId {
						exists = true
						break
					}
				}
				if !exists {
					checkURLs = append(checkURLs, JsonOutputVars{Id: urlInfoFromMap.DocId, OutUrl: urlInfoFromMap.NewUrl, ConfURL: oldUrl})
				}
				break
			}
		}
	}
	return checkURLs
}

func (m Migrator) markRegexFunc(documentData DocumentData, checkStringJSON []JsonOutputVars) []JsonOutputVars {
	if (m.markRegex != "") && strings.Contains(documentData.DocBody, m.markRegex) { //TODO support regex
		for oldUrl, urlInfoFromMap := range m.urlMap { //Getting the old confluence URL with only knowing the new outline URL
			if urlInfoFromMap.DocId == documentData.DocId {
				exists := false
				for _, docInfo := range checkStringJSON {
					if docInfo.Id == urlInfoFromMap.DocId {
						exists = true
						break
					}
				}
				if !exists {
					checkStringJSON = append(checkStringJSON, JsonOutputVars{Id: urlInfoFromMap.DocId, OutUrl: urlInfoFromMap.NewUrl, ConfURL: oldUrl})
				}
				break
			}
		}
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

func outputJSON(checkStruct []JsonOutputVars, filename string) {
	file, err := os.Create(filename + ".json")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\n")

	for i, item := range checkStruct {
		item.Counter = i + 1
		err = encoder.Encode(item)
		if err != nil {
			panic(err)
		}
	}
}

func (m Migrator) migratePageRecurse(page *cf.Content, parentDocumentId string) {
	var publish = true
	exportedDoc, err := m.confluenceClient.ExportDoc(page.ID)
	if err != nil {
		panic(err)
	}
	exportedDocBytes, err := os.ReadFile("tmp/" + *exportedDoc) // just pass the file name
	if err != nil {
		fmt.Print(err)
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
	m.createPageMapping(page, importDocumentRes)

	// File import cleanup
	os.Remove("tmp/" + *exportedDoc)
	createdDocumentId := *importDocumentRes.JSON200.Data.Id                                                  //Temp cleanup
	fmt.Printf("Imported document \"%s\" (%s).\n", createdDocumentId, *importDocumentRes.JSON200.Data.Title) //Console logging

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return
	}
	fmt.Printf("Migrating %d child pages of %s (%s).\n", page.Children.Pages.Size, page.ID, page.Title) //Console logging

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
	titleSlug := slug.Make(title)                         // Slug is not present for input document response
	newUrl := fmt.Sprintf(`/doc/%s-%s`, titleSlug, urlId) // Outline URL
	confluenceURLs := m.getPossibleConfluenceURLs(page)
	for i := range confluenceURLs {
		m.urlMap = updateUrlMap(m.urlMap, confluenceURLs[i], newUrl, createdDocumentId.String())
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

func (m Migrator) saveUrlMapToFile() { //For debug purposes saves URL map to json file. Function call can be commented out.

	urlMapJSON, err := json.MarshalIndent(m.urlMap, "", " ")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(urlMapJSON))
	file, err := os.Create("urlMap.json")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	_, err = file.Write(urlMapJSON)
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
