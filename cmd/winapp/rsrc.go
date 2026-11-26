package main

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"slices"

	"github.com/nullentropy/caution/internal/appicon"
)

// the only way to get resources into the .exe is a COFF object with a .rsrc
// section. its resource tree nests type, then id, then language, then data

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtManifest  = 24

	langEnUS = 0x0409

	imageScnCntInitializedData = 0x00000040
	imageScnMemRead            = 0x40000000
	imageSymClassStatic        = 3

	relAmd64Addr32NB = 0x0003
	relArm64Addr32NB = 0x0002

	machineAmd64 = 0x8664
	machineArm64 = 0xAA64
)

var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

type resource struct {
	typ, id uint16
	data    []byte
}

// icon images may be PNG, which Windows accepts since Vista. the group gets
// id 1 because Explorer shows the lowest-numbered group as the app icon
func iconResources(master *image.RGBA) ([]resource, error) {
	group := &bytes.Buffer{}
	binary.Write(group, binary.LittleEndian, uint16(0))
	binary.Write(group, binary.LittleEndian, uint16(1))
	binary.Write(group, binary.LittleEndian, uint16(len(iconSizes)))

	var out []resource
	for i, size := range iconSizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, appicon.Scale(master, size)); err != nil {
			return nil, fmt.Errorf("encoding %dpx icon: %w", size, err)
		}
		id := uint16(i + 1)
		out = append(out, resource{typ: rtIcon, id: id, data: buf.Bytes()})

		// GRPICONDIRENTRY size fields are one byte, so 256 is stored as 0
		dim := byte(size)
		if size >= 256 {
			dim = 0
		}
		group.WriteByte(dim)
		group.WriteByte(dim)
		group.WriteByte(0)
		group.WriteByte(0)
		binary.Write(group, binary.LittleEndian, uint16(1))
		binary.Write(group, binary.LittleEndian, uint16(32))
		binary.Write(group, binary.LittleEndian, uint32(buf.Len()))
		binary.Write(group, binary.LittleEndian, id)
	}
	return append(out, resource{typ: rtGroupIcon, id: 1, data: group.Bytes()}), nil
}

// without per-monitor DPI awareness Windows bitmap-scales the window on
// HiDPI and every glyph goes soft
func manifestXML(appID, version string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="` + appID + `" version="` + version + `" processorArchitecture="*"/>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">permonitorv2,permonitor</dpiAwareness>
      <activeCodePage xmlns="http://schemas.microsoft.com/SMI/2019/WindowsSettings">UTF-8</activeCodePage>
    </windowsSettings>
  </application>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
    </application>
  </compatibility>
</assembly>
`)
}

func buildSyso(res []resource, arch string) []byte {
	machine, relType := uint16(machineAmd64), uint16(relAmd64Addr32NB)
	if arch == "arm64" {
		machine, relType = machineArm64, relArm64Addr32NB
	}

	// Windows requires resource directory entries in sorted order
	types := map[uint16][]resource{}
	var typeOrder []uint16
	for _, r := range res {
		if _, seen := types[r.typ]; !seen {
			typeOrder = append(typeOrder, r.typ)
		}
		types[r.typ] = append(types[r.typ], r)
	}
	slices.Sort(typeOrder)
	for _, t := range typeOrder {
		slices.SortStableFunc(types[t], func(a, b resource) int { return cmp.Compare(a.id, b.id) })
	}

	const dirSize, entrySize, dataEntrySize = 16, 8, 16
	size := dirSize + entrySize*len(typeOrder)
	for _, t := range typeOrder {
		size += dirSize + entrySize*len(types[t])
		size += (dirSize + entrySize) * len(types[t])
	}
	dataEntriesAt := size
	size += dataEntrySize * len(res)
	payloadAt := size

	var dirs, dataEntries, payloads bytes.Buffer
	var relocs []uint32

	idDirAt := dirSize + entrySize*len(typeOrder)
	writeDir(&dirs, len(typeOrder))
	off := idDirAt
	for _, t := range typeOrder {
		binary.Write(&dirs, binary.LittleEndian, uint32(t))
		binary.Write(&dirs, binary.LittleEndian, uint32(off)|0x80000000)
		off += dirSize + entrySize*len(types[t]) + (dirSize+entrySize)*len(types[t])
	}

	dataIndex := 0
	for _, t := range typeOrder {
		group := types[t]
		langDirAt := idDirAt + dirSize + entrySize*len(group)
		writeDir(&dirs, len(group))
		for i := range group {
			binary.Write(&dirs, binary.LittleEndian, uint32(group[i].id))
			binary.Write(&dirs, binary.LittleEndian, uint32(langDirAt+i*(dirSize+entrySize))|0x80000000)
		}
		for range group {
			writeDir(&dirs, 1)
			binary.Write(&dirs, binary.LittleEndian, uint32(langEnUS))
			binary.Write(&dirs, binary.LittleEndian, uint32(dataEntriesAt+dataIndex*dataEntrySize))
			dataIndex++
		}
		idDirAt = langDirAt + (dirSize+entrySize)*len(group)
	}

	// OffsetToData is an RVA that the linker patches, so each entry needs a
	// relocation against the section symbol
	payloadOff := payloadAt
	for _, t := range typeOrder {
		for _, r := range types[t] {
			relocs = append(relocs, uint32(dataEntriesAt+dataEntries.Len()))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(payloadOff))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(len(r.data)))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(0))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(0))
			payloads.Write(r.data)
			for payloads.Len()%8 != 0 {
				payloads.WriteByte(0)
			}
			payloadOff = payloadAt + payloads.Len()
		}
	}

	section := append(append(dirs.Bytes(), dataEntries.Bytes()...), payloads.Bytes()...)

	const fileHeaderSize, sectionHeaderSize, relocSize, symbolSize = 20, 40, 10, 18
	sectionDataAt := fileHeaderSize + sectionHeaderSize
	relocAt := sectionDataAt + len(section)
	symbolAt := relocAt + relocSize*len(relocs)

	out := &bytes.Buffer{}
	binary.Write(out, binary.LittleEndian, machine)
	binary.Write(out, binary.LittleEndian, uint16(1))
	binary.Write(out, binary.LittleEndian, uint32(0))
	binary.Write(out, binary.LittleEndian, uint32(symbolAt))
	binary.Write(out, binary.LittleEndian, uint32(1))
	binary.Write(out, binary.LittleEndian, uint16(0))
	binary.Write(out, binary.LittleEndian, uint16(0))

	out.WriteString(".rsrc\x00\x00\x00")
	binary.Write(out, binary.LittleEndian, uint32(0))
	binary.Write(out, binary.LittleEndian, uint32(0))
	binary.Write(out, binary.LittleEndian, uint32(len(section)))
	binary.Write(out, binary.LittleEndian, uint32(sectionDataAt))
	binary.Write(out, binary.LittleEndian, uint32(relocAt))
	binary.Write(out, binary.LittleEndian, uint32(0))
	binary.Write(out, binary.LittleEndian, uint16(len(relocs)))
	binary.Write(out, binary.LittleEndian, uint16(0))
	binary.Write(out, binary.LittleEndian, uint32(imageScnCntInitializedData|imageScnMemRead))

	out.Write(section)
	for _, r := range relocs {
		binary.Write(out, binary.LittleEndian, r)
		binary.Write(out, binary.LittleEndian, uint32(0))
		binary.Write(out, binary.LittleEndian, relType)
	}
	out.WriteString(".rsrc\x00\x00\x00")
	binary.Write(out, binary.LittleEndian, uint32(0))
	binary.Write(out, binary.LittleEndian, uint16(1))
	binary.Write(out, binary.LittleEndian, uint16(0))
	out.WriteByte(imageSymClassStatic)
	out.WriteByte(0)
	binary.Write(out, binary.LittleEndian, uint32(4))

	return out.Bytes()
}

func writeDir(buf *bytes.Buffer, idEntries int) {
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(idEntries))
}
