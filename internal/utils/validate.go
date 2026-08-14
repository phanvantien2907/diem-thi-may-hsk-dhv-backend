package utils

import "github.com/go-playground/validator/v10"

// Validate is a shared, cached validator instance.
// Use utils.Validate.Struct(dto) in handlers.
var Validate = validator.New()
