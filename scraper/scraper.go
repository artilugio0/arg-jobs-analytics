package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

const geoIdArgentina = "100446943"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "output file not specified\n")
		os.Exit(1)
	}
	dataDir := os.Args[1]

	httpClient := &http.Client{}
	accessToken := os.Getenv("LINKEDIN_TOKEN")

	limiter := rate.NewLimiter(10, 1)

	listings := jobListings(httpClient, limiter, accessToken, "data scientist")

	var wg sync.WaitGroup

	jobs := []*JobPosting{}

	for jid := range listings {
		wg.Add(1)

		go func() {
			defer wg.Done()

			limiter.Wait(context.TODO())
			log.Printf("Fetching data for job %s\n", jid)

			job, err := jobPostings(httpClient, limiter, jid, accessToken)
			if err != nil {
				log.Printf("could not get job posting for jog %s: %v", jid, err)
				return
			}

			jobs = append(jobs, job)
		}()
	}

	wg.Wait()

	if err := saveJobsToFile(jobs, dataDir); err != nil {
		log.Fatal("could not save jobs: %v", err)
	}
}

type JobID = string

type JobPosting struct {
	JobID       string `json:"job_id"`
	Company     string `json:"company"`
	Description string `json:"description"`
	Title       string `json:"title"`
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

func jobListings(httpClient *http.Client, limiter *rate.Limiter, accessToken, search string) <-chan JobID {
	result := make(chan JobID)

	go func() {
		defer close(result)

		start := 0
		count := 100
		done := false

		for !done {
			url := jobListingsUrl(geoIdArgentina, start, count)
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

func authRequest(req *http.Request, accessToken string) {
	req.Header.Add("Csrf-Token", "csrf-token")
	req.AddCookie(&http.Cookie{Name: "JSESSIONID", Value: "csrf-token"})
	req.AddCookie(&http.Cookie{Name: "li_at", Value: accessToken})
}

func saveJobsToFile(jobs []*JobPosting, jobsFilePath string) error {
	dir := filepath.Dir(jobsFilePath)
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("could not create directory '%s': %v", dir, err)
		}
	}

	f, err := os.OpenFile(jobsFilePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("could not open file '%s': %v", jobsFilePath, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(jobs); err != nil {
		return fmt.Errorf("could not encode jobs to json: %v", err)
	}

	return nil
}

func jobPostingsUrl(jid JobID) string {
	return "https://www.linkedin.com/voyager/api/jobs/jobPostings/" + jid + "?decorationId=com.linkedin.voyager.deco.jobs.web.shared.WebFullJobPosting-65&topN=1&topNRequestedFlavors=List(TOP_APPLICANT,IN_NETWORK,COMPANY_RECRUIT,SCHOOL_RECRUIT,HIDDEN_GEM,ACTIVELY_HIRING_COMPANY)"
}

func jobListingsUrl(geoId string, start, count int) string {
	return fmt.Sprintf("https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards?decorationId=com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220&q=jobSearch&query=(origin:JOB_SEARCH_PAGE_LOCATION_AUTOCOMPLETE,keywords:%%22data%%20scientist%%22,locationUnion:(geoId:%s))&start=%d&count=%d", geoIdArgentina, start, count)
}
