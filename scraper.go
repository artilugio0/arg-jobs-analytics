package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

const geoIdArgentina = "100446943"

func main() {
	httpClient := &http.Client{}
	accessToken := os.Getenv("LINKEDIN_TOKEN")

	listings := jobListings(httpClient, accessToken, "data scientist")

	for jid := range listings {
		fmt.Printf("Fetching data for job %s\n", jid)
		job, err := jobPostings(httpClient, jid, accessToken)
		if err != nil {
			log.Printf("could not get job posting for jog %s: %v", jid, err)
		}

		fmt.Printf("Company: %s\tJob:%s\t(%s)\n", job.Company, job.Title, jid)
	}
}

type JobID = string

type JobPosting struct {
	Company     string
	Description string
	Title       string
}

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

func jobListings(httpClient *http.Client, accessToken, search string) <-chan JobID {
	result := make(chan JobID)

	go func() {
		defer close(result)

		start := 0
		count := 25
		done := false

		for !done {
			url := fmt.Sprintf("https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards?decorationId=com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220&q=jobSearch&query=(origin:JOB_SEARCH_PAGE_LOCATION_AUTOCOMPLETE,keywords:%%22data%%20scientist%%22,locationUnion:(geoId:%s))&start=%d&count=%d", geoIdArgentina, start, count)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Printf("error creating jobListings request: %v", err)
				return
			}
			authRequest(req, accessToken)

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

func jobPostings(httpClient *http.Client, jid JobID, accessToken string) (*JobPosting, error) {
	req, err := http.NewRequest("GET", "https://www.linkedin.com/voyager/api/jobs/jobPostings/"+jid+"?decorationId=com.linkedin.voyager.deco.jobs.web.shared.WebFullJobPosting-65&topN=1&topNRequestedFlavors=List(TOP_APPLICANT,IN_NETWORK,COMPANY_RECRUIT,SCHOOL_RECRUIT,HIDDEN_GEM,ACTIVELY_HIRING_COMPANY)", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating jobPostings request: %v", err)
	}
	authRequest(req, accessToken)

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
		Company:     content.CompanyDetails.Company.Result.Name,
		Description: content.Description.Text,
		Title:       content.Title,
	}, nil
}

func authRequest(req *http.Request, accessToken string) {
	req.Header.Add("Csrf-Token", "csrf-token")
	req.AddCookie(&http.Cookie{Name: "JSESSIONID", Value: "csrf-token"})
	req.AddCookie(&http.Cookie{Name: "li_at", Value: accessToken})
}
