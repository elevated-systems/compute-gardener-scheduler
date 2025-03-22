package regions

// GCPRegionInfo maps GCP regions to relevant grid and location information
var GCPRegionInfo = map[string]RegionInfo{
	// North America
	"us-west1": {
		ElectricityMapsZone: "US-NW-PACW",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "PACW",
		DefaultPUE:          1.09, // Google reports very low PUE
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-west1",
		Metadata: map[string]string{
			"state":        "Oregon",
			"city":         "The Dalles",
			"renewables":   "high",
			"carbonStatus": "low",
		},
	},
	"us-west2": {
		ElectricityMapsZone: "US-CAL-CISO",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "CAISO",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-west2",
		Metadata: map[string]string{
			"state": "California",
			"city":  "Los Angeles",
		},
	},
	"us-west3": {
		ElectricityMapsZone: "US-CAL-CISO",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "CAISO",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-west3",
		Metadata: map[string]string{
			"state": "Utah",
			"city":  "Salt Lake City",
		},
	},
	"us-west4": {
		ElectricityMapsZone: "US-SW-NEVP",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "NEVP",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-west4",
		Metadata: map[string]string{
			"state": "Nevada",
			"city":  "Las Vegas",
		},
	},
	"us-central1": {
		ElectricityMapsZone: "US-MIDW-MISO",
		TimeZone:            "America/Chicago",
		ISO:                 "MISO",
		DefaultPUE:          1.11,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-central1",
		Metadata: map[string]string{
			"state": "Iowa",
			"city":  "Council Bluffs",
		},
	},
	"us-east1": {
		ElectricityMapsZone: "US-SOCO",
		TimeZone:            "America/New_York",
		ISO:                 "SOCO",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-east1",
		Metadata: map[string]string{
			"state": "South Carolina",
			"city":  "Moncks Corner",
		},
	},
	"us-east4": {
		ElectricityMapsZone: "US-PJM",
		TimeZone:            "America/New_York",
		ISO:                 "PJM",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-east4",
		Metadata: map[string]string{
			"state": "Virginia",
			"city":  "Ashburn",
		},
	},
	"us-east5": {
		ElectricityMapsZone: "US-PJM",
		TimeZone:            "America/New_York",
		ISO:                 "PJM",
		DefaultPUE:          1.1,
		Country:             "US",
		CloudProvider:       "gcp",
		CloudRegion:         "us-east5",
		Metadata: map[string]string{
			"state": "Ohio",
			"city":  "Columbus",
		},
	},
	"northamerica-northeast1": {
		ElectricityMapsZone: "CA-QC",
		TimeZone:            "America/Montreal",
		ISO:                 "IESO",
		DefaultPUE:          1.11,
		Country:             "CA",
		CloudProvider:       "gcp",
		CloudRegion:         "northamerica-northeast1",
		Metadata: map[string]string{
			"province":     "Quebec",
			"city":         "Montreal",
			"renewables":   "high",
			"carbonStatus": "low",
		},
	},
	"northamerica-northeast2": {
		ElectricityMapsZone: "CA-ON",
		TimeZone:            "America/Toronto",
		ISO:                 "IESO",
		DefaultPUE:          1.12,
		Country:             "CA",
		CloudProvider:       "gcp",
		CloudRegion:         "northamerica-northeast2",
		Metadata: map[string]string{
			"province": "Ontario",
			"city":     "Toronto",
		},
	},

	// Europe
	"europe-north1": {
		ElectricityMapsZone: "FI",
		TimeZone:            "Europe/Helsinki",
		ISO:                 "FI",
		DefaultPUE:          1.07, // Cooler climate enables better efficiency
		Country:             "FI",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-north1",
		Metadata: map[string]string{
			"city":         "Hamina",
			"renewables":   "high",
			"carbonStatus": "low",
		},
	},
	"europe-west1": {
		ElectricityMapsZone: "BE",
		TimeZone:            "Europe/Brussels",
		ISO:                 "ELIA",
		DefaultPUE:          1.1,
		Country:             "BE",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west1",
		Metadata: map[string]string{
			"city": "Saint-Ghislain",
		},
	},
	"europe-west2": {
		ElectricityMapsZone: "GB",
		TimeZone:            "Europe/London",
		ISO:                 "GB",
		DefaultPUE:          1.11,
		Country:             "GB",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west2",
		Metadata: map[string]string{
			"city": "London",
		},
	},
	"europe-west3": {
		ElectricityMapsZone: "DE",
		TimeZone:            "Europe/Berlin",
		ISO:                 "DE",
		DefaultPUE:          1.1,
		Country:             "DE",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west3",
		Metadata: map[string]string{
			"city": "Frankfurt",
		},
	},
	"europe-west4": {
		ElectricityMapsZone: "NL",
		TimeZone:            "Europe/Amsterdam",
		ISO:                 "NL",
		DefaultPUE:          1.09,
		Country:             "NL",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west4",
		Metadata: map[string]string{
			"city": "Eemshaven",
		},
	},
	"europe-west6": {
		ElectricityMapsZone: "CH",
		TimeZone:            "Europe/Zurich",
		ISO:                 "CH",
		DefaultPUE:          1.09,
		Country:             "CH",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west6",
		Metadata: map[string]string{
			"city": "Zurich",
		},
	},
	"europe-west8": {
		ElectricityMapsZone: "IT",
		TimeZone:            "Europe/Rome",
		ISO:                 "IT",
		DefaultPUE:          1.12,
		Country:             "IT",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west8",
		Metadata: map[string]string{
			"city": "Milan",
		},
	},
	"europe-west9": {
		ElectricityMapsZone: "FR",
		TimeZone:            "Europe/Paris",
		ISO:                 "RTE",
		DefaultPUE:          1.1,
		Country:             "FR",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-west9",
		Metadata: map[string]string{
			"city": "Paris",
		},
	},
	"europe-southwest1": {
		ElectricityMapsZone: "ES",
		TimeZone:            "Europe/Madrid",
		ISO:                 "ES",
		DefaultPUE:          1.12,
		Country:             "ES",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-southwest1",
		Metadata: map[string]string{
			"city": "Madrid",
		},
	},
	"europe-central2": {
		ElectricityMapsZone: "PL",
		TimeZone:            "Europe/Warsaw",
		ISO:                 "PL",
		DefaultPUE:          1.14,
		Country:             "PL",
		CloudProvider:       "gcp",
		CloudRegion:         "europe-central2",
		Metadata: map[string]string{
			"city": "Warsaw",
		},
	},

	// Asia Pacific
	"asia-east1": {
		ElectricityMapsZone: "TW",
		TimeZone:            "Asia/Taipei",
		ISO:                 "TW",
		DefaultPUE:          1.2,
		Country:             "TW",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-east1",
		Metadata: map[string]string{
			"city": "Changhua County",
		},
	},
	"asia-east2": {
		ElectricityMapsZone: "HK",
		TimeZone:            "Asia/Hong_Kong",
		ISO:                 "HK",
		DefaultPUE:          1.21,
		Country:             "HK",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-east2",
		Metadata: map[string]string{
			"city": "Hong Kong",
		},
	},
	"asia-northeast1": {
		ElectricityMapsZone: "JP",
		TimeZone:            "Asia/Tokyo",
		ISO:                 "JEPX",
		DefaultPUE:          1.14,
		Country:             "JP",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-northeast1",
		Metadata: map[string]string{
			"city": "Tokyo",
		},
	},
	"asia-northeast2": {
		ElectricityMapsZone: "JP-KS",
		TimeZone:            "Asia/Tokyo",
		ISO:                 "JEPX",
		DefaultPUE:          1.14,
		Country:             "JP",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-northeast2",
		Metadata: map[string]string{
			"city": "Osaka",
		},
	},
	"asia-northeast3": {
		ElectricityMapsZone: "KR",
		TimeZone:            "Asia/Seoul",
		ISO:                 "KPX",
		DefaultPUE:          1.15,
		Country:             "KR",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-northeast3",
		Metadata: map[string]string{
			"city": "Seoul",
		},
	},
	"asia-southeast1": {
		ElectricityMapsZone: "SG",
		TimeZone:            "Asia/Singapore",
		ISO:                 "EMA",
		DefaultPUE:          1.23, // Higher due to tropical climate
		Country:             "SG",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-southeast1",
		Metadata: map[string]string{
			"city": "Singapore",
		},
	},
	"asia-southeast2": {
		ElectricityMapsZone: "ID",
		TimeZone:            "Asia/Jakarta",
		ISO:                 "ID",
		DefaultPUE:          1.25, // Higher due to tropical climate
		Country:             "ID",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-southeast2",
		Metadata: map[string]string{
			"city": "Jakarta",
		},
	},
	"asia-south1": {
		ElectricityMapsZone: "IN-NO",
		TimeZone:            "Asia/Kolkata",
		ISO:                 "IEX",
		DefaultPUE:          1.27, // Higher due to climate
		Country:             "IN",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-south1",
		Metadata: map[string]string{
			"city": "Mumbai",
		},
	},
	"asia-south2": {
		ElectricityMapsZone: "IN-NO",
		TimeZone:            "Asia/Kolkata",
		ISO:                 "IEX",
		DefaultPUE:          1.27, // Higher due to climate
		Country:             "IN",
		CloudProvider:       "gcp",
		CloudRegion:         "asia-south2",
		Metadata: map[string]string{
			"city": "Delhi",
		},
	},
	"australia-southeast1": {
		ElectricityMapsZone: "AU-NSW",
		TimeZone:            "Australia/Sydney",
		ISO:                 "AEMO",
		DefaultPUE:          1.15,
		Country:             "AU",
		CloudProvider:       "gcp",
		CloudRegion:         "australia-southeast1",
		Metadata: map[string]string{
			"city":  "Sydney",
			"state": "New South Wales",
		},
	},
	"australia-southeast2": {
		ElectricityMapsZone: "AU-VIC",
		TimeZone:            "Australia/Melbourne",
		ISO:                 "AEMO",
		DefaultPUE:          1.15,
		Country:             "AU",
		CloudProvider:       "gcp",
		CloudRegion:         "australia-southeast2",
		Metadata: map[string]string{
			"city":  "Melbourne",
			"state": "Victoria",
		},
	},

	// South America
	"southamerica-east1": {
		ElectricityMapsZone: "BR-CS",
		TimeZone:            "America/Sao_Paulo",
		ISO:                 "ONS",
		DefaultPUE:          1.25, // Higher due to climate
		Country:             "BR",
		CloudProvider:       "gcp",
		CloudRegion:         "southamerica-east1",
		Metadata: map[string]string{
			"city": "Sao Paulo",
		},
	},
	"southamerica-west1": {
		ElectricityMapsZone: "CL",
		TimeZone:            "America/Santiago",
		ISO:                 "CL",
		DefaultPUE:          1.23,
		Country:             "CL",
		CloudProvider:       "gcp",
		CloudRegion:         "southamerica-west1",
		Metadata: map[string]string{
			"city": "Santiago",
		},
	},

	// Middle East
	"me-west1": {
		ElectricityMapsZone: "IL",
		TimeZone:            "Asia/Jerusalem",
		ISO:                 "IL",
		DefaultPUE:          1.2,
		Country:             "IL",
		CloudProvider:       "gcp",
		CloudRegion:         "me-west1",
		Metadata: map[string]string{
			"city": "Tel Aviv",
		},
	},
	"me-central1": {
		ElectricityMapsZone: "QA",
		TimeZone:            "Asia/Qatar",
		ISO:                 "QA",
		DefaultPUE:          1.28, // Higher due to hot climate
		Country:             "QA",
		CloudProvider:       "gcp",
		CloudRegion:         "me-central1",
		Metadata: map[string]string{
			"city": "Doha",
		},
	},
}
