package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// --- Configuration Constants ---

// The model to use.
const MODEL_NAME = "gemini-2.5-flash-lite"

// The maximum allowed tokens per request to Gemini.
const MAX_TOKENS_PER_REQUEST = 15000

// A common ratio for estimating tokens from characters (rough estimate: 4 characters per token).
const TOKEN_TO_CHAR_RATIO = 4

// Estimated overhead for the fixed system prompt and the JSON schema.
const SYSTEM_OVERHEAD_TOKENS = 2500

// --- Data Structures ---

// JobInput represents a job object in the input JSON file.
type JobInput struct {
	JobID       string `json:"job_id"`
	Description string `json:"description"`
}

// JobAnalysis represents the desired structured output for a single job.
// NOTE: Field names are intentionally lowercase to match the requested JSON schema keys.
type JobAnalysis struct {
	JobID                string   `json:"job_id"`
	Seniority            string   `json:"seniority"`
	MandatorySkills      []string `json:"mandatory_skills"`
	NiceToHaveSkills     []string `json:"nice_to_have_skills"`
	MandatoryExperience  []string `json:"mandatory_experience"`
	NiceToHaveExperience []string `json:"nice_to_have_experience"`
	OnsiteHybridRemote   string   `json:"onsite_hybrid_remote"`
}

// --- Main Logic ---

func main() {
	// 1. Setup and Validation
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run job_analyzer.go <path/to/input.json>")
		os.Exit(1)
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("ERROR: GEMINI_API_KEY environment variable not set.")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		fmt.Printf("ERROR creating Gemini client: %v\n", err)
		os.Exit(1)
	}

	// 2. Read Input File
	inputFilePath := os.Args[1]
	jobs, err := readJobsFromFile(inputFilePath)
	if err != nil {
		fmt.Printf("ERROR reading input file: %v\n", err)
		os.Exit(1)
	}
	log.Printf("Successfully loaded %d job descriptions from %s.\n", len(jobs), inputFilePath)

	// 3. Batching
	batches := createBatches(jobs)
	log.Printf("Created %d batches for API calls based on token limit.\n", len(batches))

	// 4. Processing Batches
	var finalResults []JobAnalysis
	for i, batch := range batches {
		log.Printf("Processing batch %d/%d (containing %d jobs)...\n", i+1, len(batches), len(batch))

		batchResults, err := processBatch(ctx, client, batch)
		if err != nil {
			log.Printf("ERROR processing batch %d: %v. Skipping batch.\n", i+1, err)
			continue
		}

		finalResults = append(finalResults, batchResults...)
	}

	// 5. Output Final Results
	finalJSON, err := json.MarshalIndent(finalResults, "", "  ")
	if err != nil {
		log.Printf("ERROR marshalling final results: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(finalJSON))
}

// readJobsFromFile reads the input JSON file and unmarshals it into a slice of JobInput.
func readJobsFromFile(filePath string) ([]JobInput, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var jobs []JobInput
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// createBatches groups jobs into batches based on a calculated maximum character limit.
func createBatches(jobs []JobInput) [][]JobInput {
	// Calculate the maximum characters allowed for the *input* descriptions
	maxInputTokens := MAX_TOKENS_PER_REQUEST - SYSTEM_OVERHEAD_TOKENS
	maxInputChars := maxInputTokens * TOKEN_TO_CHAR_RATIO
	if maxInputChars <= 0 {
		fmt.Printf("Warning: Calculated max input characters is non-positive (%d). Using a default of 4000.\n", maxInputChars)
		maxInputChars = 4000
	}

	fmt.Printf("Maximum estimated input characters per request: %d (approx %d tokens).\n", maxInputChars, maxInputTokens)

	var batches [][]JobInput
	var currentBatch []JobInput
	currentBatchCharCount := 0

	for _, job := range jobs {
		jobCharCount := len(job.Description)

		// If adding the current job description exceeds the limit, finalize the current batch
		if currentBatchCharCount+jobCharCount > maxInputChars && len(currentBatch) > 0 {
			batches = append(batches, currentBatch)
			currentBatch = nil
			currentBatchCharCount = 0
		}

		// Add the job to the current batch
		currentBatch = append(currentBatch, job)
		currentBatchCharCount += jobCharCount
	}

	// Add the last batch if it's not empty
	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// processBatch sends a batch of job descriptions to the Gemini API and parses the array response.
func processBatch(ctx context.Context, client *genai.Client, batchJobs []JobInput) ([]JobAnalysis, error) {
	// 1. Construct the combined prompt
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Analyze the following job descriptions and provide the analysis for ALL of them. The jobs are separated by '---JOBBREAK---'.\n\n")

	// Append all job descriptions and their IDs
	for i, job := range batchJobs {
		promptBuilder.WriteString(fmt.Sprintf("JobID: %s\nDescription:\n%s\n", job.JobID, job.Description))
		if i < len(batchJobs)-1 {
			promptBuilder.WriteString("\n---JOBBREAK---\n\n")
		}
	}

	// 2. Define the System Instruction
	systemInstruction := `You are an expert job market analyst. Your task is to extract structured data from the provided job descriptions.
You MUST return a single JSON array containing an analysis object for every job provided in the input.

IMPORTANT: the answer MUST have EXACTLY ONE object per JobID.

Crucial formatting rules:
1. Ensure the "job_id" field in the output matches the "Job ID" from the input.
2. For all array fields (skills and experience), each item MUST be a single, atomic, machine-readable keyword or concept.
   - DO NOT use full sentences, verbose explanations, or parenthetical remarks.
   - Example (Good): "GCP", "Kubernetes", "Data Modeling".
   - Example (Bad): "Experience with Cloud technologies (AWS/Azure)", "Must have 5+ years of experience in the industry".
3. Use only the allowed enum values for "onsite_hybrid_remote": "On Site", "Hybrid", or "Remote".
4. Use only the allowed enum values for "seniority": "Junior", "Semisenior", or "Senior".
5. You must ONLY use information explicitly present or clearly implied by the job text. 
	**If information for any field other than 'job_id' is NOT found, you MUST omit that field entirely** from the JSON object. 
	For array fields (skills and experience), if no items are found, the model must return an **empty array (\[])** or omit the field. 
	DO NOT make up, infer, or hallucinate any missing data. Keep all array values concise and in lowercase. 
`

	// 3. Define the JSON Schema using the SDK's schema package
	schema := &genai.Schema{
		Type: genai.TypeArray,
		Items: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"job_id": {
					Type:        genai.TypeString,
					Description: "Job ID, must match the input Job ID.",
				},
				"seniority": {
					Type:        genai.TypeString,
					Description: "The seniority level of the job.",
					Enum:        []string{"Junior", "Semisenior", "Senior"},
				},
				"mandatory_skills": {
					Type:        genai.TypeArray,
					Description: "List of skills that are mandatory for the job. Use atomic keywords (e.g., 'Python', 'React', 'Terraform').",
					Items:       &genai.Schema{Type: genai.TypeString},
				},
				"nice_to_have_skills": {
					Type:        genai.TypeArray,
					Description: "List of skills that are nice to have but not mandatory. Use atomic keywords.",
					Items:       &genai.Schema{Type: genai.TypeString},
				},
				"mandatory_experience": {
					Type:        genai.TypeArray,
					Description: "List of experiences that are mandatory for the job. Use atomic keywords (e.g., '3 years', 'Financial Sector', 'Team Leadership').",
					Items:       &genai.Schema{Type: genai.TypeString},
				},
				"nice_to_have_experience": {
					Type:        genai.TypeArray,
					Description: "List of experiences that are nice to have but not mandatory. Use atomic keywords.",
					Items:       &genai.Schema{Type: genai.TypeString},
				},
				"onsite_hybrid_remote": {
					Type:        genai.TypeString,
					Description: "The work arrangement for the job.",
					Enum:        []string{"On Site", "Hybrid", "Remote"},
				},
			},
			Required: []string{"job_id"},
		},
	}

	// 5. Call the API (SDK handles retry/backoff logic for most transient errors)
	var resp *genai.GenerateContentResponse
	var lastErr error
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, lastErr = client.Models.GenerateContent(ctx,
			MODEL_NAME,
			genai.Text(promptBuilder.String()),
			&genai.GenerateContentConfig{
				ResponseMIMEType:  "application/json",
				ResponseSchema:    schema,
				SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemInstruction}}},
			},
		)
		if lastErr == nil {
			break // Success
		}

		log.Printf("Attempt %d failed: %v. Retrying in %v...\n", attempt+1, lastErr, time.Second*(1<<attempt))
		time.Sleep(time.Second * (1 << attempt)) // Exponential backoff
	}

	if lastErr != nil {
		return nil, fmt.Errorf("gemini API call failed after %d attempts: %w", maxRetries, lastErr)
	}

	// 6. Extract and Parse the JSON content
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini API returned no candidates or content in response")
	}

	var batchAnalysis []JobAnalysis
	if err := json.Unmarshal([]byte(resp.Text()), &batchAnalysis); err != nil {
		// Log the problematic JSON for debugging
		log.Printf("ERROR: Failed to unmarshal the model's JSON output. Raw output:\n%s\n", resp.Text())
		return nil, fmt.Errorf("failed to unmarshal model's JSON output: %w", err)
	}

	log.Printf("Batch processed successfully. Received analysis for %d jobs.\n", len(batchAnalysis))
	return batchAnalysis, nil
}
