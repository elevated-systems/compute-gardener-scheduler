package almanac

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// mockHTTPClient is a mock implementation of HTTPClient for testing
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestClient_GetScore(t *testing.T) {
	tests := []struct {
		name           string
		request        ScoreRequest
		mockResponse   *ScoreResult
		mockStatusCode int
		mockError      error
		wantErr        bool
		wantScore      float64
	}{
		{
			name: "successful score request with provider+region",
			request: ScoreRequest{
				Provider:     "aws",
				Region:       "us-west-1",
				InstanceType: "m5.xlarge",
				Weights: map[string]float64{
					"carbon": 0.5,
					"price":  0.5,
				},
			},
			mockResponse: &ScoreResult{
				Zone:              "US-CAL-CISO",
				OptimizationScore: 0.706765,
				Components: ScoreComponents{
					CarbonScore: 0.732727,
					PriceScore:  0.680804,
					BlendWeights: map[string]float64{
						"carbon": 0.5,
						"price":  0.5,
					},
				},
				RawValues: RawSignals{
					CarbonIntensity: 197,
					SpotPrice:       0.0715,
					OnDemandPrice:   0.224,
					InstanceType:    "m5.xlarge",
				},
				Recommendation: RecommendProceed,
				Timestamp:      time.Now(),
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantScore:      0.706765,
		},
		{
			name: "successful score request with zone",
			request: ScoreRequest{
				Zone: "US-CAL-CISO",
				Weights: map[string]float64{
					"carbon": 0.7,
					"price":  0.3,
				},
			},
			mockResponse: &ScoreResult{
				Zone:              "US-CAL-CISO",
				OptimizationScore: 0.85,
				Components: ScoreComponents{
					CarbonScore: 0.9,
					PriceScore:  0.7,
					BlendWeights: map[string]float64{
						"carbon": 0.7,
						"price":  0.3,
					},
				},
				RawValues: RawSignals{
					CarbonIntensity: 150,
				},
				Recommendation: RecommendOptimal,
				Timestamp:      time.Now(),
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantScore:      0.85,
		},
		{
			name: "low score with WAIT recommendation",
			request: ScoreRequest{
				Provider: "aws",
				Region:   "us-east-1",
				Weights: map[string]float64{
					"carbon": 0.8,
					"price":  0.2,
				},
			},
			mockResponse: &ScoreResult{
				Zone:              "US-MIDA-PJM",
				OptimizationScore: 0.35,
				Components: ScoreComponents{
					CarbonScore: 0.3,
					PriceScore:  0.5,
					BlendWeights: map[string]float64{
						"carbon": 0.8,
						"price":  0.2,
					},
				},
				RawValues: RawSignals{
					CarbonIntensity: 450,
					SpotPrice:       0.05,
				},
				Recommendation: RecommendWait,
				Timestamp:      time.Now(),
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			wantScore:      0.35,
		},
		{
			name: "API error response",
			request: ScoreRequest{
				Provider: "aws",
				Region:   "invalid-region",
				Weights: map[string]float64{
					"carbon": 0.5,
					"price":  0.5,
				},
			},
			mockStatusCode: http.StatusBadRequest,
			wantErr:        true,
		},
		{
			name: "missing zone and provider+region",
			request: ScoreRequest{
				Weights: map[string]float64{
					"carbon": 0.5,
					"price":  0.5,
				},
			},
			wantErr: true, // Should fail validation
		},
		{
			name: "invalid weights - don't sum to 1.0",
			request: ScoreRequest{
				Zone: "US-CAL-CISO",
				Weights: map[string]float64{
					"carbon": 0.5,
					"price":  0.7,
				},
			},
			wantErr: true, // Should fail validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockHTTPClient{
				doFunc: func(req *http.Request) (*http.Response, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}

					if tt.mockStatusCode != http.StatusOK {
						errorResp := struct {
							Error   string `json:"error"`
							Message string `json:"message"`
						}{
							Error:   "Bad Request",
							Message: "Invalid request parameters",
						}
						body, _ := json.Marshal(errorResp)
						return &http.Response{
							StatusCode: tt.mockStatusCode,
							Body:       io.NopCloser(bytes.NewReader(body)),
						}, nil
					}

					// Marshal the mock response
					body, err := json.Marshal(tt.mockResponse)
					if err != nil {
						t.Fatalf("Failed to marshal mock response: %v", err)
					}

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(body)),
					}, nil
				},
			}

			client := NewClient("http://test-almanac:8080", WithHTTPClient(mockClient))

			score, err := client.GetScore(context.Background(), tt.request)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetScore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return // Expected error, test passed
			}

			if score == nil {
				t.Fatal("GetScore() returned nil score")
			}

			if score.OptimizationScore != tt.wantScore {
				t.Errorf("GetScore() score = %v, want %v", score.OptimizationScore, tt.wantScore)
			}

			if tt.mockResponse != nil && score.Recommendation != tt.mockResponse.Recommendation {
				t.Errorf("GetScore() recommendation = %v, want %v",
					score.Recommendation, tt.mockResponse.Recommendation)
			}
		})
	}
}

func TestScoreResult_ShouldProceed(t *testing.T) {
	tests := []struct {
		name           string
		recommendation Recommendation
		want           bool
	}{
		{
			name:           "PROCEED recommendation",
			recommendation: RecommendProceed,
			want:           true,
		},
		{
			name:           "OPTIMAL recommendation",
			recommendation: RecommendOptimal,
			want:           true,
		},
		{
			name:           "WAIT recommendation",
			recommendation: RecommendWait,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := &ScoreResult{
				Recommendation: tt.recommendation,
			}
			if got := sr.ShouldProceed(); got != tt.want {
				t.Errorf("ScoreResult.ShouldProceed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreResult_IsOptimal(t *testing.T) {
	optimal := &ScoreResult{Recommendation: RecommendOptimal}
	if !optimal.IsOptimal() {
		t.Error("Expected IsOptimal() to return true for OPTIMAL recommendation")
	}

	proceed := &ScoreResult{Recommendation: RecommendProceed}
	if proceed.IsOptimal() {
		t.Error("Expected IsOptimal() to return false for PROCEED recommendation")
	}
}

func TestScoreResult_ShouldWait(t *testing.T) {
	wait := &ScoreResult{Recommendation: RecommendWait}
	if !wait.ShouldWait() {
		t.Error("Expected ShouldWait() to return true for WAIT recommendation")
	}

	proceed := &ScoreResult{Recommendation: RecommendProceed}
	if proceed.ShouldWait() {
		t.Error("Expected ShouldWait() to return false for PROCEED recommendation")
	}
}
