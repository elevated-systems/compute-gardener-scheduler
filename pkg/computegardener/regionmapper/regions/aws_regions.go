package regions

// AWSRegionInfo maps AWS regions to relevant grid and location information
var AWSRegionInfo = map[string]RegionInfo{
	// North America
	"us-east-1": {
		ElectricityMapsZone: "US-PJM",
		TimeZone:            "America/New_York",
		ISO:                 "PJM",
		DefaultPUE:          1.2,
		Country:             "US",
		CloudProvider:       "aws",
		CloudRegion:         "us-east-1",
		Metadata: map[string]string{
			"state": "Virginia",
			"city":  "Ashburn",
		},
	},
	"us-east-2": {
		ElectricityMapsZone: "US-PJM",
		TimeZone:            "America/New_York",
		ISO:                 "PJM",
		DefaultPUE:          1.2,
		Country:             "US",
		CloudProvider:       "aws",
		CloudRegion:         "us-east-2",
		Metadata: map[string]string{
			"state": "Ohio",
			"city":  "Columbus",
		},
	},
	"us-west-1": {
		ElectricityMapsZone: "US-CAL-CISO",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "CAISO",
		DefaultPUE:          1.25,
		Country:             "US",
		CloudProvider:       "aws",
		CloudRegion:         "us-west-1",
		Metadata: map[string]string{
			"state": "California",
			"city":  "San Francisco",
		},
	},
	"us-west-2": {
		ElectricityMapsZone: "US-NW-PACW",
		TimeZone:            "America/Los_Angeles",
		ISO:                 "PACW",
		DefaultPUE:          1.15,
		Country:             "US",
		CloudProvider:       "aws",
		CloudRegion:         "us-west-2",
		Metadata: map[string]string{
			"state":        "Oregon",
			"city":         "Portland",
			"renewables":   "high",
			"carbonStatus": "low",
		},
	},
	"ca-central-1": {
		ElectricityMapsZone: "CA-ON",
		TimeZone:            "America/Toronto",
		ISO:                 "IESO",
		DefaultPUE:          1.2,
		Country:             "CA",
		CloudProvider:       "aws",
		CloudRegion:         "ca-central-1",
		Metadata: map[string]string{
			"province": "Ontario",
			"city":     "Montreal",
		},
	},
	"us-central-1": {
		ElectricityMapsZone: "US-MIDW-MISO",
		TimeZone:            "America/Chicago",
		ISO:                 "MISO",
		DefaultPUE:          1.2,
		Country:             "US",
		CloudProvider:       "aws",
		CloudRegion:         "us-central-1",
		Metadata: map[string]string{
			"state": "Iowa",
		},
	},

	// Europe
	"eu-west-1": {
		ElectricityMapsZone: "IE",
		TimeZone:            "Europe/Dublin",
		ISO:                 "SEM",
		DefaultPUE:          1.2,
		Country:             "IE",
		CloudProvider:       "aws",
		CloudRegion:         "eu-west-1",
		Metadata: map[string]string{
			"city": "Dublin",
		},
	},
	"eu-west-2": {
		ElectricityMapsZone: "GB",
		TimeZone:            "Europe/London",
		ISO:                 "GB",
		DefaultPUE:          1.2,
		Country:             "GB",
		CloudProvider:       "aws",
		CloudRegion:         "eu-west-2",
		Metadata: map[string]string{
			"city": "London",
		},
	},
	"eu-west-3": {
		ElectricityMapsZone: "FR",
		TimeZone:            "Europe/Paris",
		ISO:                 "RTE",
		DefaultPUE:          1.2,
		Country:             "FR",
		CloudProvider:       "aws",
		CloudRegion:         "eu-west-3",
		Metadata: map[string]string{
			"city": "Paris",
		},
	},
	"eu-central-1": {
		ElectricityMapsZone: "DE",
		TimeZone:            "Europe/Berlin",
		ISO:                 "DE",
		DefaultPUE:          1.2,
		Country:             "DE",
		CloudProvider:       "aws",
		CloudRegion:         "eu-central-1",
		Metadata: map[string]string{
			"city": "Frankfurt",
		},
	},
	"eu-north-1": {
		ElectricityMapsZone: "SE",
		TimeZone:            "Europe/Stockholm",
		ISO:                 "SE",
		DefaultPUE:          1.1, // Lower due to cooler climate
		Country:             "SE",
		CloudProvider:       "aws",
		CloudRegion:         "eu-north-1",
		Metadata: map[string]string{
			"city":         "Stockholm",
			"renewables":   "high",
			"carbonStatus": "low",
		},
	},

	// Asia Pacific
	"ap-northeast-1": {
		ElectricityMapsZone: "JP",
		TimeZone:            "Asia/Tokyo",
		ISO:                 "JEPX",
		DefaultPUE:          1.25,
		Country:             "JP",
		CloudProvider:       "aws",
		CloudRegion:         "ap-northeast-1",
		Metadata: map[string]string{
			"city": "Tokyo",
		},
	},
	"ap-northeast-2": {
		ElectricityMapsZone: "KR",
		TimeZone:            "Asia/Seoul",
		ISO:                 "KPX",
		DefaultPUE:          1.25,
		Country:             "KR",
		CloudProvider:       "aws",
		CloudRegion:         "ap-northeast-2",
		Metadata: map[string]string{
			"city": "Seoul",
		},
	},
	"ap-southeast-1": {
		ElectricityMapsZone: "SG",
		TimeZone:            "Asia/Singapore",
		ISO:                 "EMA",
		DefaultPUE:          1.3, // Higher due to tropical climate
		Country:             "SG",
		CloudProvider:       "aws",
		CloudRegion:         "ap-southeast-1",
		Metadata: map[string]string{
			"city": "Singapore",
		},
	},
	"ap-southeast-2": {
		ElectricityMapsZone: "AU-NSW",
		TimeZone:            "Australia/Sydney",
		ISO:                 "AEMO",
		DefaultPUE:          1.25,
		Country:             "AU",
		CloudProvider:       "aws",
		CloudRegion:         "ap-southeast-2",
		Metadata: map[string]string{
			"city":  "Sydney",
			"state": "New South Wales",
		},
	},
	"ap-south-1": {
		ElectricityMapsZone: "IN-NO",
		TimeZone:            "Asia/Kolkata",
		ISO:                 "IEX",
		DefaultPUE:          1.3, // Higher due to climate
		Country:             "IN",
		CloudProvider:       "aws",
		CloudRegion:         "ap-south-1",
		Metadata: map[string]string{
			"city": "Mumbai",
		},
	},

	// South America
	"sa-east-1": {
		ElectricityMapsZone: "BR-CS",
		TimeZone:            "America/Sao_Paulo",
		ISO:                 "ONS",
		DefaultPUE:          1.3, // Higher due to climate
		Country:             "BR",
		CloudProvider:       "aws",
		CloudRegion:         "sa-east-1",
		Metadata: map[string]string{
			"city": "Sao Paulo",
		},
	},
}
