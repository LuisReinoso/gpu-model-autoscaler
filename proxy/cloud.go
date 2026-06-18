package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type RunPodProvider struct {
	apiKey     string
	endpointID string
	gpuType    string
	client     *http.Client
}

type runpodPodRequest struct {
	Name         string `json:"name"`
	ImageName    string `json:"imageName"`
	GpuTypeID    string `json:"gpuTypeId"`
	ContainerDiskInGb int `json:"containerDiskInGb"`
	MinMemoryInGb     int `json:"minMemoryInGb"`
	MinVcpuCount      int `json:"minVcpuCount"`
	Ports        string `json:"ports"`
	Env          []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"env"`
}

type runpodPodResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func NewRunPodProvider() *RunPodProvider {
	return &RunPodProvider{
		apiKey:  os.Getenv("RUNPOD_API_KEY"),
		gpuType: getEnv("RUNPOD_GPU_TYPE", "NVIDIA RTX A4000"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (r *RunPodProvider) ScaleUp() (string, error) {
	if r.apiKey == "" {
		return "", fmt.Errorf("RUNPOD_API_KEY not set")
	}

	body := runpodPodRequest{
		Name:              fmt.Sprintf("vllm-worker-%d", time.Now().Unix()),
		ImageName:         "runpod/vllm:latest",
		GpuTypeID:         r.gpuType,
		ContainerDiskInGb: 50,
		MinMemoryInGb:     16,
		MinVcpuCount:      4,
		Ports:             "8000/http",
		Env: []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}{
			{Key: "MODEL_NAME", Value: os.Getenv("MODEL_NAME")},
			{Key: "GPU_MODE", Value: "real"},
		},
	}

	bodyJSON, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.runpod.io/graphql?api_key="+r.apiKey,
		bytes.NewBuffer(bodyJSON))
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("runpod api error: %w", err)
	}
	defer resp.Body.Close()

	var podResp runpodPodResponse
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("runpod api returned %d: %s", resp.StatusCode, string(raw))
	}

	if err := json.Unmarshal(raw, &podResp); err != nil {
		return "", fmt.Errorf("runpod response parse error: %w", err)
	}

	url := fmt.Sprintf("https://%s-8000.proxy.runpod.net", podResp.ID)
	return url, nil
}

func (r *RunPodProvider) ScaleDown(workerID string) error {
	if r.apiKey == "" {
		return fmt.Errorf("RUNPOD_API_KEY not set")
	}
	// Extract pod ID from URL or use workerID directly
	// In production, parse the pod ID and call RunPod terminate endpoint
	return nil
}

type LambdaProvider struct {
	apiKey string
	client *http.Client
}

type lambdaInstanceRequest struct {
	RegionName   string `json:"region_name"`
	InstanceType string `json:"instance_type"`
	FileSystemNames []string `json:"file_system_names"`
	Quantity     int    `json:"quantity"`
	Name         string `json:"name"`
}

type lambdaInstanceResponse struct {
	Data struct {
		InstanceIDs []string `json:"instance_ids"`
	} `json:"data"`
}

func NewLambdaProvider() *LambdaProvider {
	return &LambdaProvider{
		apiKey: os.Getenv("LAMBDA_API_KEY"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (l *LambdaProvider) ScaleUp() (string, error) {
	if l.apiKey == "" {
		return "", fmt.Errorf("LAMBDA_API_KEY not set")
	}

	body := lambdaInstanceRequest{
		RegionName:   getEnv("LAMBDA_REGION", "us-east-1"),
		InstanceType: getEnv("LAMBDA_GPU_TYPE", "gpu_1x_a10"),
		Quantity:     1,
		Name:         fmt.Sprintf("vllm-worker-%d", time.Now().Unix()),
	}

	bodyJSON, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://cloud.lambdalabs.com/api/v1/instance-operations/launch",
		bytes.NewBuffer(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(l.apiKey, "")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("lambda api error: %w", err)
	}
	defer resp.Body.Close()

	var instanceResp lambdaInstanceResponse
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("lambda api returned %d: %s", resp.StatusCode, string(raw))
	}

	if err := json.Unmarshal(raw, &instanceResp); err != nil {
		return "", fmt.Errorf("lambda response parse error: %w", err)
	}

	if len(instanceResp.Data.InstanceIDs) == 0 {
		return "", fmt.Errorf("no instance IDs returned from Lambda")
	}

	instanceID := instanceResp.Data.InstanceIDs[0]
	url := fmt.Sprintf("https://%s.cloud.lambdalabs.com:8000", instanceID)
	return url, nil
}

func (l *LambdaProvider) ScaleDown(workerID string) error {
	if l.apiKey == "" {
		return fmt.Errorf("LAMBDA_API_KEY not set")
	}
	return nil
}