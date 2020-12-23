package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/360EntSecGroup-Skylar/excelize"
	_ "github.com/Luxurioust/excelize"
	_ "github.com/tealeg/xlsx"
)

type rowItem struct {
	start int // 起始行
	end   int // 结束行
	rdst  int // 读取行起始
}

type item struct {
	Kind string `json:"type"`
}

type sepcs struct {
	Unit     string `json:"unit"`
	UnitName string `json:"unitName"`
	Size     string `json:"size"`
	Step     string `json:"step"`
	Min      string `json:"min"`
	Max      string `json:"max"`
	Item     item   `json:"item"`
}

type dataType struct {
	Kind string `json:"type"`
	Spec sepcs  `json:"specs"`
}

type phyModel struct {
	Identifier      string    `json:"identifier"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Version         string    `json:"version"`
	StaticPropList  []prop    `json:"staticProperties"`
	DynamicPropList []prop    `json:"dynamicProperties"`
	ServiceList     []service `json:"services"`
	EventList       []event   `json:"events"`
	DynamicRows     rowItem   `json:"-"`
	StaticRows      rowItem   `json:"-"`
	ServiceRows     rowItem   `json:"-"`
	EventRows       rowItem   `json:"-"`
}

type prop struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	AccessMode string   `json:"accessMode"`
	Required   string   `json:"required"`
	Desc       string   `json:"desc"`
	DataType   dataType `json:"dataType"`
}

type param struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	AccessMode string   `json:"accessMode"`
	Required   string   `json:"required"`
	Desc       string   `json:"desc"`
	DataType   dataType `json:dataType`
}

type service struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Desc     string  `json:"desc"`
	Method   string  `json:"method"`
	CallType string  `json:"callType"`
	WaitTime string  `json:"waitTime"`
	Params   []param `json:"params"`
	Results  []prop  `json:"results",omitempty`
	Row      rowItem `json:"-"`
}

type event struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Desc       string  `json:"desc"`
	Kind       string  `json:"type"`
	CallType   string  `json:"callType"`
	OutputData []prop  `json:"outputData",omitempty`
	Row        rowItem `json:"-"`
}

var gLog *log.Logger

func main() {
	fmt.Println("物模型转换工具 V1.0.1")
	gLog = log.New(os.Stdout, "\r\n", log.LstdFlags|log.Lshortfile)
	file, err := os.Create("phym.log")
	if err != nil {
		log.Println("create log file error: ", err)
	} else {
		defer file.Close()

		writers := []io.Writer{
			file,
			//os.Stdout,
		}
		gLog.SetOutput(io.MultiWriter(writers...))
	}

	for {
		fp := getStdinInput("请输入xlsx文件路径: ")
		if fp == "" {
			fmt.Println(fp)
			continue
		}
		if false == pathExists(fp) {
			fmt.Println("文件不存在，请确认！")
			continue
		}
		generatePhymode(fp)
	}
}

func pathExists(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func getStdinInput(hint string) string {
	fmt.Print(hint)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func verifyXlsx(xlsx *excelize.File, sheet string) error {
	ident := getCV(xlsx, sheet, "B", 1)
	name := getCV(xlsx, sheet, "B", 2)
	version := getCV(xlsx, sheet, "B", 3)
	desc := getCV(xlsx, sheet, "B", 4)
	if false == strings.Contains(ident, "identifier") ||
		false == strings.Contains(name, "name") ||
		false == strings.Contains(version, "version") ||
		false == strings.Contains(desc, "description") {
		return errors.New(fmt.Sprintf("xlsx sheet(%s) 不是合法的物模型!", sheet))
	} else {
		return nil
	}
}

func generatePhymode(path string) {
	xlsx, err := excelize.OpenFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	lst := xlsx.GetSheetList()
	for k, v := range lst {
		_ = k
		if strings.Contains(v, "模板") || strings.Contains(v, "使用模板填写注意事项") {
			continue
		}
		err := verifyXlsx(xlsx, v)
		if err != nil {
			fmt.Println(err)
			continue
		}
		propMap := make(map[string]prop)
		phyM := phyModel{}
		phyM.Identifier = getCV(xlsx, v, "C", 1)
		phyM.Name = getCV(xlsx, v, "C", 2)
		phyM.Version = getCV(xlsx, v, "C", 3)
		phyM.Description = getCV(xlsx, v, "C", 4)

		mge, err := xlsx.GetMergeCells(v)
		if err != nil {
			fmt.Println(err)
		} else {
			for _, mg := range mge {
				getRowItem(mg, "staticProperties", &phyM.StaticRows)
				getRowItem(mg, "dynamicProperties", &phyM.DynamicRows)
				getRowItem(mg, "services", &phyM.ServiceRows)
				getRowItem(mg, "events", &phyM.EventRows)
			}
		}

		getRowRdItem(mge, "静态属性名称", &phyM.StaticRows)
		adjustRowitem(&phyM.StaticRows)
		//gLog.Println("staticProp: ", phyM.StaticRows)
		for i := phyM.StaticRows.rdst; i <= phyM.StaticRows.end; i++ {
			staticP := prop{}
			staticP.ID = getCV(xlsx, v, "C", i)
			if staticP.ID == "" {
				continue
			}
			staticP.Name = getCV(xlsx, v, "D", i)
			staticP.Desc = getCV(xlsx, v, "E", i)
			staticP.AccessMode = getCV(xlsx, v, "F", i)
			staticP.Required = getCV(xlsx, v, "G", i)
			staticP.DataType.Kind = getCV(xlsx, v, "H", i)
			staticP.DataType.Spec.Min = getCV(xlsx, v, "I", i)
			staticP.DataType.Spec.Max = getCV(xlsx, v, "J", i)
			staticP.DataType.Spec.Unit = getCV(xlsx, v, "K", i)
			staticP.DataType.Spec.UnitName = getCV(xlsx, v, "L", i)
			staticP.DataType.Spec.Size = getCV(xlsx, v, "M", i)
			staticP.DataType.Spec.Step = getCV(xlsx, v, "N", i)
			staticP.DataType.Spec.Item.Kind = getCV(xlsx, v, "O", i)
			phyM.StaticPropList = append(phyM.StaticPropList, staticP)
			propMap[staticP.ID] = staticP
			//fmt.Println("staticProp: ", i)
		}
		if len(phyM.StaticPropList) < 1 {
			phyM.StaticPropList = make([]prop, 0)
		}

		getRowRdItem(mge, "动态属性名称", &phyM.DynamicRows)
		adjustRowitem(&phyM.DynamicRows)
		//gLog.Println("dynamicProp: ", phyM.DynamicRows)
		for i := phyM.DynamicRows.rdst; i <= phyM.DynamicRows.end; i++ {
			dynamicP := prop{}
			dynamicP.ID = getCV(xlsx, v, "C", i)
			if dynamicP.ID == "" {
				continue
			}
			dynamicP.Name = getCV(xlsx, v, "D", i)
			dynamicP.Desc = getCV(xlsx, v, "E", i)
			dynamicP.AccessMode = getCV(xlsx, v, "F", i)
			dynamicP.Required = getCV(xlsx, v, "G", i)
			dynamicP.DataType.Kind = getCV(xlsx, v, "H", i)
			dynamicP.DataType.Spec.Min = getCV(xlsx, v, "I", i)
			dynamicP.DataType.Spec.Max = getCV(xlsx, v, "J", i)
			dynamicP.DataType.Spec.Unit = getCV(xlsx, v, "K", i)
			dynamicP.DataType.Spec.UnitName = getCV(xlsx, v, "L", i)
			dynamicP.DataType.Spec.Size = getCV(xlsx, v, "M", i)
			dynamicP.DataType.Spec.Step = getCV(xlsx, v, "N", i)
			dynamicP.DataType.Spec.Item.Kind = getCV(xlsx, v, "O", i)
			phyM.DynamicPropList = append(phyM.DynamicPropList, dynamicP)
			propMap[dynamicP.ID] = dynamicP
			//fmt.Println("dymicProp: ", i)
		}
		if len(phyM.DynamicPropList) < 1 {
			phyM.DynamicPropList = make([]prop, 0)
		}

		getRowRdItem(mge, "服务名称", &phyM.ServiceRows)
		adjustRowitem(&phyM.ServiceRows)
		//gLog.Println("service: ", phyM.ServiceRows)
		lastServRowItem := phyM.ServiceRows
		lastServRowItem.end = phyM.ServiceRows.start
		for i := phyM.ServiceRows.rdst; i <= phyM.ServiceRows.end; {
			serv := service{}
			serv.ID = getCV(xlsx, v, "C", i)
			if serv.ID == "" {
				i++
				continue
			}
			//gLog.Println("service: ", i)
			serv.Name = getCV(xlsx, v, "D", i)
			serv.Desc = getCV(xlsx, v, "E", i)
			serv.Method = getCV(xlsx, v, "F", i)
			serv.CallType = getCV(xlsx, v, "G", i)
			serv.WaitTime = getCV(xlsx, v, "H", i)
			getSubRowItem(mge, serv.ID, &phyM.ServiceRows, &lastServRowItem, &serv.Row)
			adjustSubRowitem(i, &serv.Row)
			//gLog.Println("service: id=", serv.ID, ",", serv.Row)
			for k := serv.Row.start; k <= serv.Row.end; k++ {
				para := param{}
				para.ID = getCV(xlsx, v, "I", k)
				//gLog.Println("I", k, ":", para.ID)
				if para.ID != "" {
					para.Name = getCV(xlsx, v, "J", k)
					para.Desc = getCV(xlsx, v, "K", k)
					para.AccessMode = getCV(xlsx, v, "L", k)
					para.Required = getCV(xlsx, v, "M", k)
					para.DataType.Kind = getCV(xlsx, v, "N", k)
					para.DataType.Spec.Min = getCV(xlsx, v, "O", k)
					para.DataType.Spec.Max = getCV(xlsx, v, "P", k)
					para.DataType.Spec.Unit = getCV(xlsx, v, "Q", k)
					para.DataType.Spec.UnitName = getCV(xlsx, v, "R", k)
					para.DataType.Spec.Size = getCV(xlsx, v, "S", k)
					para.DataType.Spec.Step = getCV(xlsx, v, "T", k)
					para.DataType.Spec.Item.Kind = getCV(xlsx, v, "U", k)
					serv.Params = append(serv.Params, para)
				}
				restid := getCV(xlsx, v, "V", k)
				if p, ok := propMap[restid]; ok {
					serv.Results = append(serv.Results, p)
				}
				if len(serv.Results) < 1 {
					serv.Results = make([]prop, 0)
				}
			}

			phyM.ServiceList = append(phyM.ServiceList, serv)
			i = serv.Row.end + 1
			lastServRowItem = serv.Row
		}
		if len(phyM.ServiceList) < 1 {
			phyM.ServiceList = make([]service, 0)
		}

		getRowRdItem(mge, "消息名称", &phyM.EventRows)
		adjustRowitem(&phyM.EventRows)
		//gLog.Println("event: ", phyM.EventRows)
		lastEventRowItem := phyM.EventRows
		lastEventRowItem.end = phyM.EventRows.start
		for i := phyM.EventRows.rdst; i <= phyM.EventRows.end; {
			ev := event{}
			ev.ID = getCV(xlsx, v, "C", i)
			if ev.ID == "" {
				i++
				continue
			}
			//gLog.Println("event: ", i)
			ev.Name = getCV(xlsx, v, "D", i)
			ev.Desc = getCV(xlsx, v, "E", i)
			ev.Kind = getCV(xlsx, v, "F", i)
			ev.CallType = getCV(xlsx, v, "G", i)
			getSubRowItem(mge, ev.ID, &phyM.EventRows, &lastEventRowItem, &ev.Row)
			//gLog.Println("22: ", ev.Row)
			adjustSubRowitem(i, &ev.Row)

			//gLog.Println(ev.Row)
			for k := ev.Row.start; k <= ev.Row.end; k++ {
				outputid := getCV(xlsx, v, "H", k)
				if outputid == "" {
					continue
				}
				if p, ok := propMap[outputid]; ok {
					ev.OutputData = append(ev.OutputData, p)
				}
			}
			if len(ev.OutputData) < 1 {
				ev.OutputData = make([]prop, 0)
			}
			phyM.EventList = append(phyM.EventList, ev)
			i = ev.Row.end + 1
			lastEventRowItem = ev.Row
		}
		if len(phyM.EventList) < 1 {
			phyM.EventList = make([]event, 0)
		}

		writeFile(&phyM, v)
	}
}

func getCV(xlsx *excelize.File, sheet, colPre string, colIdx int) string {
	val, err := xlsx.GetCellValue(sheet, fmt.Sprintf("%s%d", colPre, colIdx))
	if err != nil {
		return ""
	}
	return strings.Replace(val, "\n", " ", -1)
}

func getRowItem(mge excelize.MergeCell, item string, row *rowItem) {
	if strings.Contains(mge.GetCellValue(), item) {
		val, err := strconv.Atoi(mge.GetStartAxis()[1:])
		if err != nil {
			fmt.Println(err)
		} else if val >= 0 {
			row.start = val
		}
		val, err = strconv.Atoi(mge.GetEndAxis()[1:])
		if err != nil {
			fmt.Println(err)
		} else if val >= 0 {
			row.end = val
		}
	}
}

func getRowRdItem(mge []excelize.MergeCell, title string, row *rowItem) {
	for _, mg := range mge {
		if strings.Contains(mg.GetCellValue(), title) {
			start, err := strconv.Atoi(mg.GetStartAxis()[1:])
			if err != nil {
				fmt.Println(err)
				continue
			}
			end, err := strconv.Atoi(mg.GetEndAxis()[1:])
			if err != nil {
				fmt.Println(err)
				continue
			}
			if start < row.start || end > row.end {
				continue
			}
			row.rdst = end
			break
		}
	}
}

func getSubRowItem(mge []excelize.MergeCell, title string, row, lastRow, subRow *rowItem) {
	for _, mg := range mge {
		if 0 == strings.Compare(mg.GetCellValue(), title) {
			start, err := strconv.Atoi(mg.GetStartAxis()[1:])
			if err != nil {
				fmt.Println(err)
				continue
			}
			end, err := strconv.Atoi(mg.GetEndAxis()[1:])
			if err != nil {
				fmt.Println(err)
				continue
			}
			//gLog.Println(start, ",", end)

			if start < 1 || end < 1 {
				continue
			}
			if start < row.start || end > row.end {
				//gLog.Println("row: ", row)
				continue
			}
			if start < lastRow.end {
				//gLog.Println("lastrow: ", lastRow)
				continue
			}
			subRow.start = start
			subRow.end = end
			break
		}
	}
}

func adjustRowitem(row *rowItem) {
	if 0 == row.rdst {
		row.rdst = row.start
	}
	if row.rdst > 0 {
		row.rdst = row.rdst + 1
	}
	if row.end < row.rdst {
		row.end = row.rdst
	}
	if row.end < row.start {
		row.end = row.start
	}
}

func adjustSubRowitem(idx int, subRow *rowItem) {
	//gLog.Println("adjustSubRowitem: ", subRow.start, ", ", subRow.end)
	if 0 == subRow.rdst {
		subRow.rdst = subRow.start
	}
	if subRow.rdst > 0 {
		subRow.rdst = subRow.rdst + 1
	}
	if subRow.end < subRow.rdst {
		subRow.end = subRow.rdst
	}
	if subRow.end < subRow.start {
		subRow.end = subRow.start
	}
	if 0 == subRow.start && 0 == subRow.end {
		subRow.start = idx
		subRow.end = idx
	}
}

func getCurrentPath() string {
	execPath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return ""
	}

	// Is Symlink
	fi, err := os.Lstat(execPath)
	if err != nil {
		return ""
	}

	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		execPath, err = os.Readlink(execPath)
		if err != nil {
			return ""
		}
	}

	execDir := filepath.Dir(execPath)
	if execDir == "." {
		execDir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	return execDir
}

func createDirectory(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.Mkdir(path, 0755)
		if err != nil {
			fmt.Println("createDirectory:", err.Error())
			return false
		}
	}
	return true
}

func writeFile(phyM *phyModel, fn string) {
	data, err := json.Marshal(phyM)
	if err != nil {
		fmt.Println(err)
	} else {
		var str bytes.Buffer
		json.Indent(&str, []byte(data), "", "    ")
		dir := filepath.Join(getCurrentPath(), "物模型")
		createDirectory(dir)
		fileName := fmt.Sprintf("%s.json", fn)
		path := filepath.Join(dir, fileName)
		fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Println("writeFile openfile error:", err.Error())
		} else {
			defer fd.Close()
			fd.Write(str.Bytes())
			fd.Sync()
			fmt.Printf("generate file(%s) success\n", path)
		}
	}
}
