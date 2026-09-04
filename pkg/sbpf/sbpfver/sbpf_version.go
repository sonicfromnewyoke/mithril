package sbpfver

import "github.com/sonicfromnewyoke/mithril/pkg/features"

const (
	SbpfVersionV0 = iota
	SbpfVersionV1
	SbpfVersionV2
	SbpfVersionV3
)

type SbpfVersion struct {
	Version uint32
}

func (ver *SbpfVersion) DynamicStackFrames() bool {
	return ver.Version == SbpfVersionV1 || ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) StackFrameGaps() bool {
	return ver.Version == SbpfVersionV0
}

func (ver *SbpfVersion) RejectRodataStackOverlap() bool {
	return ver.Version != SbpfVersionV0
}

func (ver *SbpfVersion) EnableElfVAddr() bool {
	return ver.Version != SbpfVersionV0
}

func (ver *SbpfVersion) EnablePqr() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) ExplicitSignExtensionOfResults() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) SwapSubRegImmOperands() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) DisableNeg() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) CallXUsesSrcReg() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) CallXUsesDstReg() bool {
	return ver.Version >= SbpfVersionV3
}

func (ver *SbpfVersion) DisableLe() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) DisableLddw() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) MoveMemoryInstructionClasses() bool {
	return ver.Version == SbpfVersionV2
}

func (ver *SbpfVersion) EnableStaticSyscalls() bool {
	return ver.Version >= SbpfVersionV3
}

func (ver *SbpfVersion) EnableStricterElfHeaders() bool {
	return ver.Version >= SbpfVersionV3
}

func (ver *SbpfVersion) EnableStricterVerification() bool {
	return false
}

func (ver *SbpfVersion) EnableLowerRodataVaddr() bool {
	return ver.Version >= SbpfVersionV3
}

func (ver *SbpfVersion) EnableLowerBytecodeVaddr() bool {
	return false
}

func (ver *SbpfVersion) EnableJmp32() bool {
	return ver.Version >= SbpfVersionV3
}

func (ver *SbpfVersion) CalculateCallImmTargetPC(pc int64, imm int32) int64 {
	if ver.EnableStaticSyscalls() {
		return pc + int64(imm) + 1
	}
	return int64(uint32(imm))
}

func GetMinAndMaxSbpfVersions(f *features.Features) (uint32, uint32) {
	disableSbpfV0 := f.IsActive(features.DisableSbpfV0Execution)
	reenableSbpfV0 := f.IsActive(features.ReenableSbpfV0Execution)
	enableSbpfV0 := !disableSbpfV0 || reenableSbpfV0
	enableSbpfV1 := f.IsActive(features.EnableSbpfV1DeploymentAndExecution)
	enableSbpfV2 := f.IsActive(features.EnableSbpfV2DeploymentAndExecution)
	enableSbpfV3 := f.IsSbpfV3DeploymentAndExecutionActive()

	var maxVer, minVer uint32

	// minimum version
	if enableSbpfV0 {
		minVer = SbpfVersionV0
	} else {
		minVer = SbpfVersionV3
	}

	// max version
	if enableSbpfV3 {
		maxVer = SbpfVersionV3
	} else if enableSbpfV2 {
		maxVer = SbpfVersionV2
	} else if enableSbpfV1 {
		maxVer = SbpfVersionV1
	} else {
		maxVer = SbpfVersionV0
	}

	return minVer, maxVer
}
