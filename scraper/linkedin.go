package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/time/rate"
)

type jobPostingsResponse struct {
	CompanyDetails struct {
		Company struct {
			Result struct {
				Name string `json:"name"`
			} `json:"companyResolutionResult"`
		} `json:"com.linkedin.voyager.deco.jobs.web.shared.WebJobPostingCompany"`
	}
	Description struct {
		Text string `json:"text"`
	} `json:"description"`
	Title string `json:"title"`
}

type jobListingsResponse struct {
	Metadata struct {
		JobCardPrefetchQueries []struct {
			PrefetchJobPostingCardUrns []string `json:"prefetchJobPostingCardUrns"`
		} `json:"jobCardPrefetchQueries"`
	} `json:"metadata"`
	Paging struct {
		Total int `json:"total"`
		Start int `json:"start"`
		Count int `json:"count"`
	} `json:"paging"`
}

func jobListings(httpClient *http.Client, limiter *rate.Limiter, accessToken, search string) <-chan JobID {
	result := make(chan JobID)

	go func() {
		defer close(result)

		start := 0
		count := 100
		done := false

		for !done {
			url := jobListingsUrl(search, geoIdArgentina, start, count)
			fmt.Println(url)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Printf("error creating jobListings request: %v", err)
				return
			}
			authRequest(req, accessToken)

			limiter.Wait(context.TODO())
			resp, err := httpClient.Do(req)
			if err != nil {
				log.Printf("error making jobListings request: %v", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				log.Printf("error jobListings response was not OK: %d (%s)", resp.StatusCode, resp.Status)
				return
			}

			content := jobListingsResponse{}
			if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
				log.Printf("error decoding jobListings response: %v", err)
				return
			}

			for _, id := range content.Metadata.JobCardPrefetchQueries[0].PrefetchJobPostingCardUrns {
				result <- strings.ReplaceAll(strings.ReplaceAll(id, "urn:li:fsd_jobPostingCard:(", ""), ",JOB_DETAILS)", "")
			}

			start += len(content.Metadata.JobCardPrefetchQueries[0].PrefetchJobPostingCardUrns)
			done = start >= content.Paging.Total
		}
	}()

	return result
}

func jobPostings(httpClient *http.Client, limiter *rate.Limiter, jid JobID, accessToken string) (*JobPosting, error) {
	req, err := http.NewRequest("GET", jobPostingsUrl(jid), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating jobPostings request: %v", err)
	}
	authRequest(req, accessToken)

	limiter.Wait(context.TODO())

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making jobPostings request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error jobPostings response was not OK: %d (%s)", resp.StatusCode, resp.Status)
	}

	content := jobPostingsResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return nil, fmt.Errorf("error decoding jobPostings response: %v", err)
	}

	return &JobPosting{
		JobID:       jid,
		Company:     content.CompanyDetails.Company.Result.Name,
		Description: content.Description.Text,
		Title:       content.Title,
	}, nil
}

func jobPostingsUrl(jid JobID) string {
	return "https://www.linkedin.com/voyager/api/jobs/jobPostings/" + jid + "?decorationId=com.linkedin.voyager.deco.jobs.web.shared.WebFullJobPosting-65&topN=1&topNRequestedFlavors=List(TOP_APPLICANT,IN_NETWORK,COMPANY_RECRUIT,SCHOOL_RECRUIT,HIDDEN_GEM,ACTIVELY_HIRING_COMPANY)"
}

func jobListingsUrl(search, geoId string, start, count int) string {
	encodedSearch := url.QueryEscape(`"` + search + `"`)
	encodedSearch = strings.ReplaceAll(encodedSearch, "+", "%20")
	return fmt.Sprintf("https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards?decorationId=com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220&q=jobSearch&query=(origin:JOB_SEARCH_PAGE_LOCATION_AUTOCOMPLETE,keywords:%s,locationUnion:(geoId:%s))&start=%d&count=%d", encodedSearch, geoIdArgentina, start, count)
}

func authRequest(req *http.Request, accessToken string) {
	req.Header.Add("Csrf-Token", "csrf-token")
	req.AddCookie(&http.Cookie{Name: "JSESSIONID", Value: "csrf-token"})
	req.AddCookie(&http.Cookie{Name: "li_at", Value: accessToken})
}
