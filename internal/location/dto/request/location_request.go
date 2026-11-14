// Package request contains location request DTOs.
package request

// ResolveAddressRequestDTO carries raw OCR or user-supplied address text for backend resolution.
type ResolveAddressRequestDTO struct {
	Address         string `json:"address"`
	AddressProvince string `json:"address_province"`
	AddressDistrict string `json:"address_district"`
	AddressWard     string `json:"address_ward"`
	AddressDetail   string `json:"address_detail"`
}
