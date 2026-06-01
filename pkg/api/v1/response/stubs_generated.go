package response

type Frequency int32

const (
	FREQUENCY_UNSPECIFIED Frequency = 0
	FREQUENCY_DAILY Frequency = 1
	FREQUENCY_WEEKLY Frequency = 2
	FREQUENCY_BI_WEEKLY Frequency = 3
	FREQUENCY_MONTHLY Frequency = 4
	FREQUENCY_QUARTERLY Frequency = 5
	FREQUENCY_SEMI_ANNUAL Frequency = 6
	FREQUENCY_ANNUAL Frequency = 7
)
