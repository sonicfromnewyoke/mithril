package sealevel

import (
	"fmt"
	"math/big"

	bls12381 "github.com/Overclock-Validator/gnark-crypto/ecc/bls12-381"
	"github.com/Overclock-Validator/gnark-crypto/ecc/bls12-381/fr"
	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/safemath"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
)

const (
	bls12_381BigEndianFlag = 0x80

	Bls12_381LE = 4
	Bls12_381BE = Bls12_381LE | bls12_381BigEndianFlag

	Bls12_381G1LE = 5
	Bls12_381G1BE = Bls12_381G1LE | bls12_381BigEndianFlag

	Bls12_381G2LE = 6
	Bls12_381G2BE = Bls12_381G2LE | bls12_381BigEndianFlag
)

const (
	Bls12_381FqLen           = 48
	Bls12_381Fq2Len          = 96
	Bls12_381ScalarLen       = 32
	Bls12_381G1CompressedLen = 48
	Bls12_381G1Len           = 96
	Bls12_381G2CompressedLen = 96
	Bls12_381G2Len           = 192
	Bls12_381GtLen           = 576
	Bls12_381MaxPairingPairs = 8
)

func isBls12_381CurveId(curveId uint64) bool {
	switch curveId {
	case Bls12_381G1LE, Bls12_381G1BE, Bls12_381G2LE, Bls12_381G2BE:
		return true
	default:
		return false
	}
}

func bls12_381SwapFqEndianness(input []byte) []byte {
	return altbn128ReverseBytes(input, Bls12_381FqLen)
}

func bls12_381SwapG2C0C1(input []byte) []byte {
	output := make([]byte, len(input))
	copy(output, input)

	for offset := 0; offset+Bls12_381Fq2Len <= len(output); offset += Bls12_381Fq2Len {
		copy(output[offset:offset+Bls12_381FqLen], input[offset+Bls12_381FqLen:offset+Bls12_381Fq2Len])
		copy(output[offset+Bls12_381FqLen:offset+Bls12_381Fq2Len], input[offset:offset+Bls12_381FqLen])
	}

	return output
}

func bls12_381ReverseBytes(input []byte) []byte {
	output := make([]byte, len(input))
	for idx := range input {
		output[len(input)-1-idx] = input[idx]
	}
	return output
}

func bls12_381IsZeroed(head byte, tail []byte) bool {
	if head != 0 {
		return false
	}

	for _, b := range tail {
		if b != 0 {
			return false
		}
	}

	return true
}

func bls12_381PrepareG1Bytes(input []byte, littleEndian bool) []byte {
	if littleEndian {
		return bls12_381SwapFqEndianness(input)
	}

	output := make([]byte, len(input))
	copy(output, input)
	return output
}

func bls12_381PrepareG2Bytes(input []byte, littleEndian bool) []byte {
	if littleEndian {
		return bls12_381SwapG2C0C1(bls12_381SwapFqEndianness(input))
	}

	output := make([]byte, len(input))
	copy(output, input)
	return output
}

func bls12_381DecodeG1Point(input []byte, littleEndian bool, subgroupCheck bool) (*bls12381.G1Affine, error) {
	if len(input) != Bls12_381G1Len {
		return nil, fmt.Errorf("invalid G1 input length")
	}

	buf := bls12_381PrepareG1Bytes(input, littleEndian)
	if buf[0]&0xa0 != 0 {
		return nil, fmt.Errorf("invalid G1 point encoding")
	}

	point := new(bls12381.G1Affine)
	if buf[0]&0x40 != 0 {
		if !bls12_381IsZeroed(buf[0]&^byte(0xe0), buf[1:]) {
			return nil, fmt.Errorf("invalid G1 infinity encoding")
		}

		point.SetInfinity()
		return point, nil
	}

	if err := point.X.SetBytesCanonical(buf[:Bls12_381FqLen]); err != nil {
		return nil, err
	}
	if err := point.Y.SetBytesCanonical(buf[Bls12_381FqLen:Bls12_381G1Len]); err != nil {
		return nil, err
	}
	if !point.IsOnCurve() {
		return nil, fmt.Errorf("invalid G1 point")
	}
	if subgroupCheck && !point.IsInSubGroup() {
		return nil, fmt.Errorf("invalid G1 subgroup")
	}

	return point, nil
}

func bls12_381DecodeG2Point(input []byte, littleEndian bool, subgroupCheck bool) (*bls12381.G2Affine, error) {
	if len(input) != Bls12_381G2Len {
		return nil, fmt.Errorf("invalid G2 input length")
	}

	buf := bls12_381PrepareG2Bytes(input, littleEndian)
	if buf[0]&0xa0 != 0 {
		return nil, fmt.Errorf("invalid G2 point encoding")
	}

	point := new(bls12381.G2Affine)
	if buf[0]&0x40 != 0 {
		if !bls12_381IsZeroed(buf[0]&^byte(0xe0), buf[1:]) {
			return nil, fmt.Errorf("invalid G2 infinity encoding")
		}

		point.SetInfinity()
		return point, nil
	}

	if err := point.X.A1.SetBytesCanonical(buf[:Bls12_381FqLen]); err != nil {
		return nil, err
	}
	if err := point.X.A0.SetBytesCanonical(buf[Bls12_381FqLen:Bls12_381Fq2Len]); err != nil {
		return nil, err
	}
	if err := point.Y.A1.SetBytesCanonical(buf[Bls12_381Fq2Len : Bls12_381Fq2Len+Bls12_381FqLen]); err != nil {
		return nil, err
	}
	if err := point.Y.A0.SetBytesCanonical(buf[Bls12_381Fq2Len+Bls12_381FqLen : Bls12_381G2Len]); err != nil {
		return nil, err
	}
	if !point.IsOnCurve() {
		return nil, fmt.Errorf("invalid G2 point")
	}
	if subgroupCheck && !point.IsInSubGroup() {
		return nil, fmt.Errorf("invalid G2 subgroup")
	}

	return point, nil
}

func bls12_381DecodeScalar(input []byte, littleEndian bool) (*big.Int, error) {
	if len(input) != Bls12_381ScalarLen {
		return nil, fmt.Errorf("invalid scalar input length")
	}

	scalarBytes := make([]byte, len(input))
	copy(scalarBytes, input)
	if littleEndian {
		scalarBytes = bls12_381ReverseBytes(scalarBytes)
	}

	var scalar fr.Element
	if err := scalar.SetBytesCanonical(scalarBytes); err != nil {
		return nil, err
	}

	return scalar.BigInt(new(big.Int)), nil
}

func bls12_381MarshalG1Point(point *bls12381.G1Affine, littleEndian bool) []byte {
	raw := point.RawBytes()
	if littleEndian {
		return bls12_381SwapFqEndianness(raw[:])
	}
	return raw[:]
}

func bls12_381MarshalG2Point(point *bls12381.G2Affine, littleEndian bool) []byte {
	raw := point.RawBytes()
	if littleEndian {
		return bls12_381SwapFqEndianness(bls12_381SwapG2C0C1(raw[:]))
	}
	return raw[:]
}

func bls12_381MarshalGt(gt *bls12381.GT, littleEndian bool) []byte {
	raw := gt.Bytes()
	if littleEndian {
		return bls12_381ReverseBytes(raw[:])
	}
	return raw[:]
}

func bls12_381G1ValidateWithEndianness(input []byte, littleEndian bool) bool {
	_, err := bls12_381DecodeG1Point(input, littleEndian, true)
	return err == nil
}

func bls12_381G2ValidateWithEndianness(input []byte, littleEndian bool) bool {
	_, err := bls12_381DecodeG2Point(input, littleEndian, true)
	return err == nil
}

func bls12_381G1DecompressWithEndianness(input []byte, littleEndian bool) ([]byte, error) {
	if len(input) != Bls12_381G1CompressedLen {
		return nil, fmt.Errorf("invalid G1 compressed length")
	}

	buf := bls12_381PrepareG1Bytes(input, littleEndian)
	var point bls12381.G1Affine
	if _, err := point.SetBytes(buf); err != nil {
		return nil, err
	}

	return bls12_381MarshalG1Point(&point, littleEndian), nil
}

func bls12_381G2DecompressWithEndianness(input []byte, littleEndian bool) ([]byte, error) {
	if len(input) != Bls12_381G2CompressedLen {
		return nil, fmt.Errorf("invalid G2 compressed length")
	}

	buf := bls12_381PrepareG2Bytes(input, littleEndian)
	var point bls12381.G2Affine
	if _, err := point.SetBytes(buf); err != nil {
		return nil, err
	}

	return bls12_381MarshalG2Point(&point, littleEndian), nil
}

func bls12_381G1AdditionWithEndianness(leftInput, rightInput []byte, littleEndian bool) ([]byte, error) {
	leftPoint, err := bls12_381DecodeG1Point(leftInput, littleEndian, false)
	if err != nil {
		return nil, err
	}
	rightPoint, err := bls12_381DecodeG1Point(rightInput, littleEndian, false)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G1Affine).Add(leftPoint, rightPoint)
	return bls12_381MarshalG1Point(result, littleEndian), nil
}

func bls12_381G1SubtractionWithEndianness(leftInput, rightInput []byte, littleEndian bool) ([]byte, error) {
	leftPoint, err := bls12_381DecodeG1Point(leftInput, littleEndian, false)
	if err != nil {
		return nil, err
	}
	rightPoint, err := bls12_381DecodeG1Point(rightInput, littleEndian, false)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G1Affine).Sub(leftPoint, rightPoint)
	return bls12_381MarshalG1Point(result, littleEndian), nil
}

func bls12_381G1MultiplicationWithEndianness(pointInput, scalarInput []byte, littleEndian bool) ([]byte, error) {
	point, err := bls12_381DecodeG1Point(pointInput, littleEndian, true)
	if err != nil {
		return nil, err
	}
	scalar, err := bls12_381DecodeScalar(scalarInput, littleEndian)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G1Affine).ScalarMultiplication(point, scalar)
	return bls12_381MarshalG1Point(result, littleEndian), nil
}

func bls12_381G2AdditionWithEndianness(leftInput, rightInput []byte, littleEndian bool) ([]byte, error) {
	leftPoint, err := bls12_381DecodeG2Point(leftInput, littleEndian, false)
	if err != nil {
		return nil, err
	}
	rightPoint, err := bls12_381DecodeG2Point(rightInput, littleEndian, false)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G2Affine).Add(leftPoint, rightPoint)
	return bls12_381MarshalG2Point(result, littleEndian), nil
}

func bls12_381G2SubtractionWithEndianness(leftInput, rightInput []byte, littleEndian bool) ([]byte, error) {
	leftPoint, err := bls12_381DecodeG2Point(leftInput, littleEndian, false)
	if err != nil {
		return nil, err
	}
	rightPoint, err := bls12_381DecodeG2Point(rightInput, littleEndian, false)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G2Affine).Sub(leftPoint, rightPoint)
	return bls12_381MarshalG2Point(result, littleEndian), nil
}

func bls12_381G2MultiplicationWithEndianness(pointInput, scalarInput []byte, littleEndian bool) ([]byte, error) {
	point, err := bls12_381DecodeG2Point(pointInput, littleEndian, true)
	if err != nil {
		return nil, err
	}
	scalar, err := bls12_381DecodeScalar(scalarInput, littleEndian)
	if err != nil {
		return nil, err
	}

	result := new(bls12381.G2Affine).ScalarMultiplication(point, scalar)
	return bls12_381MarshalG2Point(result, littleEndian), nil
}

func bls12_381PairingMapWithEndianness(g1PointsInput, g2PointsInput []byte, littleEndian bool) ([]byte, error) {
	if len(g1PointsInput)%Bls12_381G1Len != 0 || len(g2PointsInput)%Bls12_381G2Len != 0 {
		return nil, fmt.Errorf("invalid pairing input length")
	}

	numPairs := len(g1PointsInput) / Bls12_381G1Len
	if numPairs != len(g2PointsInput)/Bls12_381G2Len {
		return nil, fmt.Errorf("pairing input length mismatch")
	}
	if numPairs > Bls12_381MaxPairingPairs {
		return nil, fmt.Errorf("too many pairing inputs")
	}

	if numPairs == 0 {
		var identity bls12381.GT
		identity.SetOne()
		return bls12_381MarshalGt(&identity, littleEndian), nil
	}

	g1Affines := make([]bls12381.G1Affine, 0, numPairs)
	g2Affines := make([]bls12381.G2Affine, 0, numPairs)

	for idx := 0; idx < numPairs; idx++ {
		g1Point, err := bls12_381DecodeG1Point(g1PointsInput[idx*Bls12_381G1Len:(idx+1)*Bls12_381G1Len], littleEndian, true)
		if err != nil {
			return nil, err
		}
		g2Point, err := bls12_381DecodeG2Point(g2PointsInput[idx*Bls12_381G2Len:(idx+1)*Bls12_381G2Len], littleEndian, true)
		if err != nil {
			return nil, err
		}

		g1Affines = append(g1Affines, *g1Point)
		g2Affines = append(g2Affines, *g2Point)
	}

	result, err := bls12381.Pair(g1Affines, g2Affines)
	if err != nil {
		return nil, err
	}

	return bls12_381MarshalGt(&result, littleEndian), nil
}

func handleBls12_381G1GroupOps(vm sbpf.VM, littleEndian bool, groupOp, leftInputAddr, rightInputAddr, resultPointAddr uint64) (uint64, error) {
	execCtx := executionCtx(vm)

	var cost uint64
	var result []byte
	var err error

	switch groupOp {
	case CurveOpAdd:
		cost = cu.CUBls12_381G1AddCost
	case CurveOpSub:
		cost = cu.CUBls12_381G1SubCost
	case CurveOpMul:
		cost = cu.CUBls12_381G1MulCost
	default:
		return syscallErrCustom("SyscallError::InvalidAttribute")
	}

	if err = execCtx.ComputeMeter.Consume(cost); err != nil {
		return syscallCuErr()
	}

	switch groupOp {
	case CurveOpAdd, CurveOpSub:
		leftInput, err := vm.Translate(leftInputAddr, Bls12_381G1Len, false)
		if err != nil {
			return syscallErr(err)
		}
		rightInput, err := vm.Translate(rightInputAddr, Bls12_381G1Len, false)
		if err != nil {
			return syscallErr(err)
		}

		if groupOp == CurveOpAdd {
			result, err = bls12_381G1AdditionWithEndianness(leftInput, rightInput, littleEndian)
		} else {
			result, err = bls12_381G1SubtractionWithEndianness(leftInput, rightInput, littleEndian)
		}
	case CurveOpMul:
		scalarInput, err := vm.Translate(leftInputAddr, Bls12_381ScalarLen, false)
		if err != nil {
			return syscallErr(err)
		}
		pointInput, err := vm.Translate(rightInputAddr, Bls12_381G1Len, false)
		if err != nil {
			return syscallErr(err)
		}

		result, err = bls12_381G1MultiplicationWithEndianness(pointInput, scalarInput, littleEndian)
	}

	if err != nil {
		return syscallSuccess(1)
	}

	resultPointSlice, err := vm.Translate(resultPointAddr, Bls12_381G1Len, true)
	if err != nil {
		return syscallErr(err)
	}

	copy(resultPointSlice, result)
	return syscallSuccess(0)
}

func handleBls12_381G2GroupOps(vm sbpf.VM, littleEndian bool, groupOp, leftInputAddr, rightInputAddr, resultPointAddr uint64) (uint64, error) {
	execCtx := executionCtx(vm)

	var cost uint64
	var result []byte
	var err error

	switch groupOp {
	case CurveOpAdd:
		cost = cu.CUBls12_381G2AddCost
	case CurveOpSub:
		cost = cu.CUBls12_381G2SubCost
	case CurveOpMul:
		cost = cu.CUBls12_381G2MulCost
	default:
		return syscallErrCustom("SyscallError::InvalidAttribute")
	}

	if err = execCtx.ComputeMeter.Consume(cost); err != nil {
		return syscallCuErr()
	}

	switch groupOp {
	case CurveOpAdd, CurveOpSub:
		leftInput, err := vm.Translate(leftInputAddr, Bls12_381G2Len, false)
		if err != nil {
			return syscallErr(err)
		}
		rightInput, err := vm.Translate(rightInputAddr, Bls12_381G2Len, false)
		if err != nil {
			return syscallErr(err)
		}

		if groupOp == CurveOpAdd {
			result, err = bls12_381G2AdditionWithEndianness(leftInput, rightInput, littleEndian)
		} else {
			result, err = bls12_381G2SubtractionWithEndianness(leftInput, rightInput, littleEndian)
		}
	case CurveOpMul:
		scalarInput, err := vm.Translate(leftInputAddr, Bls12_381ScalarLen, false)
		if err != nil {
			return syscallErr(err)
		}
		pointInput, err := vm.Translate(rightInputAddr, Bls12_381G2Len, false)
		if err != nil {
			return syscallErr(err)
		}

		result, err = bls12_381G2MultiplicationWithEndianness(pointInput, scalarInput, littleEndian)
	}

	if err != nil {
		return syscallSuccess(1)
	}

	resultPointSlice, err := vm.Translate(resultPointAddr, Bls12_381G2Len, true)
	if err != nil {
		return syscallErr(err)
	}

	copy(resultPointSlice, result)
	return syscallSuccess(0)
}

func SyscallCurveDecompressImpl(vm sbpf.VM, curveId, pointAddr, resultAddr uint64) (uint64, error) {
	execCtx := executionCtx(vm)

	switch curveId {
	case Bls12_381G1LE, Bls12_381G1BE:
		if err := execCtx.ComputeMeter.Consume(cu.CUBls12_381G1DecompressCost); err != nil {
			return syscallCuErr()
		}

		compressedPoint, err := vm.Translate(pointAddr, Bls12_381G1CompressedLen, false)
		if err != nil {
			return syscallErr(err)
		}
		resultPoint, err := vm.Translate(resultAddr, Bls12_381G1Len, true)
		if err != nil {
			return syscallErr(err)
		}

		decompressedPoint, err := bls12_381G1DecompressWithEndianness(compressedPoint, curveId == Bls12_381G1LE)
		if err != nil {
			return syscallSuccess(1)
		}

		copy(resultPoint, decompressedPoint)
		return syscallSuccess(0)

	case Bls12_381G2LE, Bls12_381G2BE:
		if err := execCtx.ComputeMeter.Consume(cu.CUBls12_381G2DecompressCost); err != nil {
			return syscallCuErr()
		}

		compressedPoint, err := vm.Translate(pointAddr, Bls12_381G2CompressedLen, false)
		if err != nil {
			return syscallErr(err)
		}
		resultPoint, err := vm.Translate(resultAddr, Bls12_381G2Len, true)
		if err != nil {
			return syscallErr(err)
		}

		decompressedPoint, err := bls12_381G2DecompressWithEndianness(compressedPoint, curveId == Bls12_381G2LE)
		if err != nil {
			return syscallSuccess(1)
		}

		copy(resultPoint, decompressedPoint)
		return syscallSuccess(0)

	default:
		return syscallErrCustom("SyscallError::InvalidAttribute")
	}
}

var SyscallCurveDecompress = sbpf.SyscallFunc3(SyscallCurveDecompressImpl)

func SyscallCurvePairingMapImpl(vm sbpf.VM, curveId, numPairs, g1PointsAddr, g2PointsAddr, resultAddr uint64) (uint64, error) {
	execCtx := executionCtx(vm)

	switch curveId {
	case Bls12_381LE, Bls12_381BE:
		var cost uint64 = cu.CUBls12_381PairingOnePairCost
		cost = safemath.SaturatingAddU64(cost, safemath.SaturatingMulU64(cu.CUBls12_381PairingAdditionalPairCost, safemath.SaturatingSubU64(numPairs, 1)))
		if err := execCtx.ComputeMeter.Consume(cost); err != nil {
			return syscallCuErr()
		}

		g1PointsLen := safemath.SaturatingMulU64(numPairs, Bls12_381G1Len)
		g2PointsLen := safemath.SaturatingMulU64(numPairs, Bls12_381G2Len)

		g1Points, err := vm.Translate(g1PointsAddr, g1PointsLen, false)
		if err != nil {
			return syscallErr(err)
		}
		g2Points, err := vm.Translate(g2PointsAddr, g2PointsLen, false)
		if err != nil {
			return syscallErr(err)
		}
		resultPoint, err := vm.Translate(resultAddr, Bls12_381GtLen, true)
		if err != nil {
			return syscallErr(err)
		}

		result, err := bls12_381PairingMapWithEndianness(g1Points, g2Points, curveId == Bls12_381LE)
		if err != nil {
			return syscallSuccess(1)
		}

		copy(resultPoint, result)
		return syscallSuccess(0)

	default:
		return syscallErrCustom("SyscallError::InvalidAttribute")
	}
}

var SyscallCurvePairingMap = sbpf.SyscallFunc5(SyscallCurvePairingMapImpl)
