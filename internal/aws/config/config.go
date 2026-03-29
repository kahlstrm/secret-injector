package config

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

// LoadDefault resolves shared AWS SDK configuration for aws_* providers.
func LoadDefault(ctx context.Context) (aws.Config, error) {
	return awscfg.LoadDefaultConfig(ctx)
}
