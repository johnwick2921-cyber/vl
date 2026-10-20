#!/usr/bin/env python3
"""Minimal filterable Excel workbook from audited CSV tables; stdlib only."""
import argparse
import csv
import re
import zipfile
from pathlib import Path
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET

NS="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
REL="http://schemas.openxmlformats.org/officeDocument/2006/relationships"


def col(n):
    s=""
    while n:
        n,r=divmod(n-1,26)
        s=chr(65+r)+s
    return s


def export(folder,output):
    specs=[("Summary","summary-table.csv"),("Monthly","monthly.csv"),
           ("All trades","all-trades.csv"),("Successful TP","successful-trades.csv"),
           ("Stopped SL","stopped-trades.csv"),("Unresolved","unresolved-trades.csv"),
           ("Excluded dates","excluded-dates.csv"),("Long Short","long-short.csv")]
    counts={}
    with zipfile.ZipFile(output,"x",zipfile.ZIP_DEFLATED) as z:
        z.writestr("_rels/.rels",'<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="'+REL+'/officeDocument" Target="xl/workbook.xml"/></Relationships>')
        content=['<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>',
                 '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>',
                 '<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>']
        wb=['<workbook xmlns="'+NS+'" xmlns:r="'+REL+'"><sheets>']
        rels=['<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">']
        for i,(name,filename) in enumerate(specs,1):
            rows=list(csv.reader((folder/filename).open()))
            counts[name]=len(rows)-1
            last=col(len(rows[0]))+str(len(rows))
            xml=['<worksheet xmlns="'+NS+'"><dimension ref="A1:'+last+'"/><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="'+str(len(rows[0]))+'" width="20" customWidth="1"/></cols><sheetData>']
            for r,row in enumerate(rows,1):
                xml.append('<row r="'+str(r)+'">')
                for c,value in enumerate(row,1):
                    ref=col(c)+str(r)
                    if r>1 and re.fullmatch(r"-?\d+(?:\.\d+)?",value):
                        xml.append('<c r="'+ref+'"><v>'+value+'</v></c>')
                    else:
                        xml.append('<c r="'+ref+'" s="'+("1" if r==1 else "0")+'" t="inlineStr"><is><t xml:space="preserve">'+escape(value)+'</t></is></c>')
                xml.append("</row>")
            xml.append('</sheetData><autoFilter ref="A1:'+last+'"/></worksheet>')
            z.writestr(f"xl/worksheets/sheet{i}.xml","".join(xml))
            wb.append(f'<sheet name="{name}" sheetId="{i}" r:id="rId{i}"/>')
            rels.append(f'<Relationship Id="rId{i}" Type="{REL}/worksheet" Target="worksheets/sheet{i}.xml"/>')
            content.append(f'<Override PartName="/xl/worksheets/sheet{i}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>')
        wb.append("</sheets></workbook>")
        rels.append(f'<Relationship Id="rId{len(specs)+1}" Type="{REL}/styles" Target="styles.xml"/></Relationships>')
        content.append("</Types>")
        z.writestr("xl/workbook.xml","".join(wb))
        z.writestr("xl/_rels/workbook.xml.rels","".join(rels))
        z.writestr("[Content_Types].xml","".join(content))
        z.writestr("xl/styles.xml",'<styleSheet xmlns="'+NS+'"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF17365D"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs></styleSheet>')
    # Re-open, CRC-verify, parse every XML, and check every row count.
    with zipfile.ZipFile(output) as z:
        assert z.testzip() is None
        for filename in z.namelist():
            ET.fromstring(z.read(filename))
        for i,(name,_) in enumerate(specs,1):
            root=ET.fromstring(z.read(f"xl/worksheets/sheet{i}.xml"))
            assert len(root.findall(f"{{{NS}}}sheetData/{{{NS}}}row"))-1==counts[name]
    print(output,counts)


if __name__=="__main__":
    p=argparse.ArgumentParser()
    p.add_argument("--tables",required=True)
    p.add_argument("--out",required=True)
    a=p.parse_args()
    export(Path(a.tables),Path(a.out))
