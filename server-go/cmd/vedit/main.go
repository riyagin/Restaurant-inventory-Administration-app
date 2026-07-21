package main
import ("fmt";"os";"github.com/xuri/excelize/v2")
func main(){
 in:=os.Getenv("IN"); out:=os.Getenv("OUT")
 f,err:=excelize.OpenFile(in); if err!=nil{fmt.Println("open:",err);return}
 defer f.Close()
 rows,_:=f.GetRows("Kebijakan")
 n:=len(rows)
 f.SetCellStr("Kebijakan","E2","9")               // change first policy's points -> 9
 r:=n+1
 f.SetCellStr("Kebijakan",fmt.Sprintf("B%d",r),"Keluar kantor tanpa izin")
 f.SetCellStr("Kebijakan",fmt.Sprintf("C%d",r),"Manual") // Indonesian label, not code
 f.SetCellStr("Kebijakan",fmt.Sprintf("E%d",r),"10")
 f.SetCellStr("Kebijakan",fmt.Sprintf("G%d",r),"ya")
 if err:=f.SaveAs(out);err!=nil{fmt.Println("save:",err);return}
 fmt.Println("ok; data rows before:",n-1)
}
