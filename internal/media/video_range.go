package media

import "strings"

type videoRangeInfo struct {
	name, compatible string
	dolbyProfile     int
}

func namedVideoRange(name string) videoRangeInfo {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dovi", "dolbyvision", "dolby vision":
		return videoRangeInfo{name: "DOVI"}
	case "doviwithhdr10", "doviwithel":
		return videoRangeInfo{name: "DOVI", compatible: "HDR10"}
	case "doviwithhdr10plus", "doviwithelhdr10plus":
		return videoRangeInfo{name: "DOVI", compatible: "HDR10+"}
	case "doviwithhlg":
		return videoRangeInfo{name: "DOVI", compatible: "HLG"}
	case "doviwithsdr":
		return videoRangeInfo{name: "DOVI", compatible: "SDR"}
	case "hdr10", "hdr 10":
		return videoRangeInfo{name: "HDR10"}
	case "hdr10plus", "hdr10+", "hdr 10+":
		return videoRangeInfo{name: "HDR10+"}
	case "hlg", "hyperloggamma":
		return videoRangeInfo{name: "HLG"}
	case "hdr":
		return videoRangeInfo{name: "HDR"}
	case "sdr":
		return videoRangeInfo{name: "SDR"}
	default:
		return videoRangeInfo{}
	}
}

func transferVideoRange(transfer, primaries string) videoRangeInfo {
	switch strings.ToLower(transfer) {
	case "smpte2084":
		if strings.EqualFold(primaries, "bt2020") {
			return videoRangeInfo{name: "HDR10"}
		}
		return videoRangeInfo{name: "HDR"}
	case "arib-std-b67":
		return videoRangeInfo{name: "HLG"}
	default:
		return videoRangeInfo{}
	}
}

func (info videoRangeInfo) withDolbyVision(profile, compatibilityID int) videoRangeInfo {
	if info.name != "DOVI" && profile <= 0 {
		return info
	}
	info.name = "DOVI"
	info.dolbyProfile = max(profile, 0)
	if info.compatible != "" {
		return info
	}
	switch {
	case profile == 7, compatibilityID == 1:
		info.compatible = "HDR10"
	case compatibilityID == 4:
		info.compatible = "HLG"
	case compatibilityID == 2:
		info.compatible = "SDR"
	}
	return info
}

func (v *Version) setVideoRange(info videoRangeInfo) {
	v.VideoRange, v.CompatibleRange, v.DolbyVisionProfile = info.name, info.compatible, info.dolbyProfile
}
