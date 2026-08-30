package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"

	"github.com/nullentropy/caution/go/internal/appicon"
)

// Windows resources live inside the .exe, and the only way to get them there
// is to hand the linker a COFF object holding a .rsrc section. This file
// builds that object: an icon group, its images, and an application
// manifest, laid out as the three-level resource tree Windows expects
// (type -> id -> language -> data).

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtManifest  = 24

	langNeutral = 0x0409 // en-US, the conventional single-language resource

	imageScnCntInitializedData = 0x00000040
	imageScnMemRead            = 0x40000000
	imageSymClassStatic        = 3

	relAmd64Addr32NB = 0x0003 // IMAGE_REL_AMD64_ADDR32NB
	relArm64Addr32NB = 0x0002 // IMAGE_REL_ARM64_ADDR32NB

	machineAmd64 = 0x8664
	machineArm64 = 0xAA64
)

// iconSizes are the images an .exe should carry: small ones for lists and
// the taskbar, large ones for the desktop and Alt-Tab. Windows picks the
// nearest and scales, so covering the common DPI steps avoids blurry icons.
var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

// resource is one leaf: a type, an id, and its bytes.
type resource struct {
	typ, id uint16
	data    []byte
}

// iconResources renders the icon at every size as PNG (Windows has accepted
// PNG-compressed icon images since Vista), then builds the group directory
// that ties them together. Icon images get ids 1..N and the group gets id 1,
// which is what makes it the application icon: Explorer shows the
// lowest-numbered group.
func iconResources(master *image.RGBA) ([]resource, error) {
	group := &bytes.Buffer{}
	binary.Write(group, binary.LittleEndian, uint16(0))              // reserved
	binary.Write(group, binary.LittleEndian, uint16(1))              // type: icon
	binary.Write(group, binary.LittleEndian, uint16(len(iconSizes))) // image count

	var out []resource
	for i, size := range iconSizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, appicon.Scale(master, size)); err != nil {
			return nil, fmt.Errorf("encoding %dpx icon: %w", size, err)
		}
		id := uint16(i + 1)
		out = append(out, resource{typ: rtIcon, id: id, data: buf.Bytes()})

		// GRPICONDIRENTRY. A 256px image records its dimensions as 0, since
		// the field is a single byte.
		dim := byte(size)
		if size >= 256 {
			dim = 0
		}
		group.WriteByte(dim)                                        // width
		group.WriteByte(dim)                                        // height
		group.WriteByte(0)                                          // palette colors
		group.WriteByte(0)                                          // reserved
		binary.Write(group, binary.LittleEndian, uint16(1))         // color planes
		binary.Write(group, binary.LittleEndian, uint16(32))        // bits per pixel
		binary.Write(group, binary.LittleEndian, uint32(buf.Len())) // bytes in image
		binary.Write(group, binary.LittleEndian, id)                // RT_ICON id
	}
	return append(out, resource{typ: rtGroupIcon, id: 1, data: group.Bytes()}), nil
}

// manifestXML declares per-monitor DPI awareness, without which Windows
// bitmap-scales the window on a HiDPI display and every glyph goes soft, and
// names the Windows versions the app was built against.
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

// buildSyso serializes resources into a COFF object the Go linker will merge
// into the .exe. The layout is header, one section header, the section's
// bytes, the relocations that fix up data pointers, and a symbol table
// naming the section.
func buildSyso(res []resource, arch string) ([]byte, error) {
	machine, relType := uint16(machineAmd64), uint16(relAmd64Addr32NB)
	if arch == "arm64" {
		machine, relType = machineArm64, relArm64Addr32NB
	}

	// Group the leaves by type, then by id, so the tree can be walked in the
	// sorted order the resource format requires.
	types := map[uint16][]resource{}
	var typeOrder []uint16
	for _, r := range res {
		if _, seen := types[r.typ]; !seen {
			typeOrder = append(typeOrder, r.typ)
		}
		types[r.typ] = append(types[r.typ], r)
	}
	sortUint16(typeOrder)
	for _, t := range typeOrder {
		sortResourcesByID(types[t])
	}

	// Directory sizes are known up front: one root, one per type, one per
	// leaf (the language level), then the data entries.
	const dirSize, entrySize, dataEntrySize = 16, 8, 16
	size := dirSize + entrySize*len(typeOrder)
	for _, t := range typeOrder {
		size += dirSize + entrySize*len(types[t])     // the id level
		size += (dirSize + entrySize) * len(types[t]) // one language dir per id
	}
	dataEntriesAt := size
	total := len(res)
	size += dataEntrySize * total
	payloadAt := size

	var dirs, dataEntries, payloads bytes.Buffer
	var relocs []struct{ off uint32 }

	// Root directory, one entry per type, each pointing at an id directory.
	idDirAt := dirSize + entrySize*len(typeOrder)
	writeDir(&dirs, len(typeOrder))
	off := idDirAt
	for _, t := range typeOrder {
		binary.Write(&dirs, binary.LittleEndian, uint32(t))
		binary.Write(&dirs, binary.LittleEndian, uint32(off)|0x80000000)
		off += dirSize + entrySize*len(types[t]) + (dirSize+entrySize)*len(types[t])
	}

	// Per type: an id directory, then one language directory per id.
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
			binary.Write(&dirs, binary.LittleEndian, uint32(langNeutral))
			binary.Write(&dirs, binary.LittleEndian, uint32(dataEntriesAt+dataIndex*dataEntrySize))
			dataIndex++
		}
		idDirAt = langDirAt + (dirSize+entrySize)*len(group)
	}

	// Data entries. OffsetToData is an RVA the linker patches, so each one
	// needs a relocation against the section symbol.
	payloadOff := payloadAt
	for _, t := range typeOrder {
		for _, r := range types[t] {
			relocs = append(relocs, struct{ off uint32 }{uint32(dataEntriesAt + dataEntries.Len())})
			binary.Write(&dataEntries, binary.LittleEndian, uint32(payloadOff))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(len(r.data)))
			binary.Write(&dataEntries, binary.LittleEndian, uint32(0)) // code page
			binary.Write(&dataEntries, binary.LittleEndian, uint32(0)) // reserved
			payloads.Write(r.data)
			for payloads.Len()%8 != 0 { // keep payloads 8-byte aligned
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
	binary.Write(out, binary.LittleEndian, uint16(machine))
	binary.Write(out, binary.LittleEndian, uint16(1))        // one section
	binary.Write(out, binary.LittleEndian, uint32(0))        // timestamp: zero keeps builds reproducible
	binary.Write(out, binary.LittleEndian, uint32(symbolAt)) // symbol table offset
	binary.Write(out, binary.LittleEndian, uint32(1))        // one symbol
	binary.Write(out, binary.LittleEndian, uint16(0))        // no optional header
	binary.Write(out, binary.LittleEndian, uint16(0))        // characteristics

	out.WriteString(".rsrc\x00\x00\x00")
	binary.Write(out, binary.LittleEndian, uint32(0))             // virtual size
	binary.Write(out, binary.LittleEndian, uint32(0))             // virtual address
	binary.Write(out, binary.LittleEndian, uint32(len(section)))  // raw size
	binary.Write(out, binary.LittleEndian, uint32(sectionDataAt)) // raw data pointer
	binary.Write(out, binary.LittleEndian, uint32(relocAt))       // relocations
	binary.Write(out, binary.LittleEndian, uint32(0))             // line numbers
	binary.Write(out, binary.LittleEndian, uint16(len(relocs)))   // relocation count
	binary.Write(out, binary.LittleEndian, uint16(0))             // line number count
	binary.Write(out, binary.LittleEndian, uint32(imageScnCntInitializedData|imageScnMemRead))

	out.Write(section)
	for _, r := range relocs {
		binary.Write(out, binary.LittleEndian, r.off)     // address to patch
		binary.Write(out, binary.LittleEndian, uint32(0)) // symbol index: the section itself
		binary.Write(out, binary.LittleEndian, relType)
	}
	out.WriteString(".rsrc\x00\x00\x00")
	binary.Write(out, binary.LittleEndian, uint32(0)) // value
	binary.Write(out, binary.LittleEndian, uint16(1)) // section number
	binary.Write(out, binary.LittleEndian, uint16(0)) // type
	out.WriteByte(imageSymClassStatic)
	out.WriteByte(0)                                  // no aux symbols
	binary.Write(out, binary.LittleEndian, uint32(4)) // empty string table

	return out.Bytes(), nil
}

func writeDir(buf *bytes.Buffer, idEntries int) {
	binary.Write(buf, binary.LittleEndian, uint32(0))         // characteristics
	binary.Write(buf, binary.LittleEndian, uint32(0))         // timestamp
	binary.Write(buf, binary.LittleEndian, uint16(0))         // major version
	binary.Write(buf, binary.LittleEndian, uint16(0))         // minor version
	binary.Write(buf, binary.LittleEndian, uint16(0))         // named entries
	binary.Write(buf, binary.LittleEndian, uint16(idEntries)) // id entries
}

func sortUint16(v []uint16) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

func sortResourcesByID(v []resource) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].id < v[j-1].id; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
