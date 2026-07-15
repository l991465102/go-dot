package v1

import "github.com/lzw7758/go-dot/internal/project"

type CreateReq = project.CreateInput
type CreateRes = project.Creation
type ListRes struct {
	Projects []project.Project `json:"projects"`
}
type RotateWriteKeyRes = project.WriteKeyRotation
