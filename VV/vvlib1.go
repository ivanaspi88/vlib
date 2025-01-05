package vv

// Идеология:
//
// Воркер - это обычная go-функция, но заточенная для работы в качестве goroutine.
//          То есть, запускается при помощи команды go <имя_воркера>
// id обработки - чтобы как-то идентифицировать каждый факт выполнения воркера (=goroutine),
//          используется так называемый id обработки. Это тупое 64ричное целое число, которое постоянно формируется и
//          (путем инкрементирования при каждом вызове) всегда наготове в канале VcPid.
//          В начале кода каждого воркера мы должны запрашивать у канала VcPid свой id и далее ссылаться на этот id
//          где необходимо внутри кода воркера.
// Логгер  - это специальная функция-воркер, которая записывает в текстовый файл (в дальнейшем это может быть база данных)
//           основные идентификационные данные неких событий приложения. Каких именно - выбираем мы сами. Чтобы записать
//           событие приложения, нужно выбрать место в коде, где его нужно зафиксировать, и вызвать метод Vlog c пояснительным текстом
//           и указанием типа события: ошибка или обычная информация. Этот метод заполнит специальную структуру для описания
//           события типа Vle и закинет её в канал событий: VcLog. Воркер логгера постоянно читает этот канал и как только
//           в нем появляется объект структуры Vle, он записывает его данные в текстовый файл событий. По умолчанию это log.txt
//           Фишка в том, что некоторые данные (имя go-файла, имя функции, номер строки кода) могут быть получены только в точке
//           вызова записи события. Поэтому и используется спец.метод Vlog, который и делает всю эту работу.
//           Пример вызова логгера: vv.Vlogger.Vlog(0, "Listener error:"+err.Error(), 1)

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------
// глобальные объявления пакета
// -----------------------------------------------------------------------------
// общий лог событий приложения
type Vlg struct {
	Level int    // max уровень логирования для всего приложения:0-off, 1-errors, 2-errors+info
	File  string // полное имя файла для записи лога
}

// Vle событие приложения
type Vle struct {
	pid    uint64    // ключ: id обработки
	event  int       // код события
	pname  string    // имя процедуры
	prc    int       // % выполнения обработки
	estr   string    // текст события
	etime  time.Time // время события
	gfile  string    // go-файл
	funame string    // имя функции
	line   int       // строка кода
	etype  int       // тип события: 0-info, 1-error
}

var Vlogger Vlg       // объект лога
var VcLog chan Vle    // канал лога приложения
var VcPid chan uint64 // канал раздачи id-обработки
var vpid uint64       // хранение очередного номера id-обработки
var Vapplt time.Time  // время запуска приложения

// -----------------------------------------------------------------------------
// Функции пакета
// init-----------------------------------------------------------------------------
// Инициализация библиотеки
func init() {
	Vapplt = time.Now()             // время запуска приложения -> глобальная переменная приложения
	go FreeMemory()                 // стартуем воркер периодического очистителя оперативной памяти
	VcLog = make(chan Vle, 1000)    // создаем буферизованный (1000 элементов типа Vle) канал для логирования событий приложения
	VcPid = make(chan uint64, 1000) // создаем буферизованный (1000 элементов типа uint64) канал для раздачи id-обработки (для идентификации воркеров)
	vpid = 0                        // исходный id-обработки, будет инкрементироваться при каждом запросе

	// задаем умолчания для имени log-файла и max уровня логирования
	if Vlogger.File == "" {
		Vlogger.Level = 2
		Vlogger.File = "log.txt"
	}

	go gopid()                              // стартуем воркер раздатчика id-обработки
	go logger()                             // стартуем воркер логгера
	Vlogger.Vlog(0, "Application start", 0) // первая запись логгера при запуске приложения, id тупо ставим = 0
	fmt.Print("Initialization completed\n") // выводим на консоль инфо-сообщение об успешном завершении инициализации приложения
}

// gopid-----------------------------------------------------------------------------
// Воркер для формирования id-обработки
// - постоянно пытается вытолкнуть в output-буфер канала очередной id-обработки
// - логика формирования id-обработки: тупой инкремент для каждого нового id
// - если output-буфер канала окажется полностью заполненным, то выполнение кода зависает на строке записи очередного id
func gopid() {
	for true {
		vpid++
		VcPid <- vpid // записываем в output-буфер канала очередной id-обработки
	}
}

// Getpid -----------------------------------------------------------------------------
// Получение id-обработки
// - запрашивается только из кода воркеров
// - если обработка ведется в основном потоке, то id всегда = 0
func Getpid() (pid uint64) {
	pid = <-VcPid
	return pid
}

// FreeMemory -----------------------------------------------------------------------------
// Воркер принудительного (каждые три секунды) запуска уборщика мусора
func FreeMemory() {
	for true {
		time.Sleep(3000 * time.Millisecond) // период выполнения
		debug.FreeOSMemory()                // вызов уборщика мусора
	}
}

// logger-----------------------------------------------------------------------------
// Воркер логирования событий приложения в текстовый файл
func logger() (err error) {
	// - логировать можно любые места в любом коде, если это не слишком затратно по времени
	// - логировать можно и в файл, и в таблицу Logger базы данных
	// - логировать нужно так, чтобы можно было выбирать
	// - данные для логирования берем из глобального канала VcLog
	// - эти данные засовывает в канал VcLog код в нужных местах при помощи функции Vlog,
	// в которую передается текст конкретного сообщения и его тип: ошибка или инфо
	Getpid() // забираем id обработки
	fmt.Print("Logger: start\n")

	// открываем файл лога
	f, er := os.OpenFile(Vlogger.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if er != nil {
		log.Println(er)
	}

	for true {
		vle := <-VcLog // получаем очередное событие приложения для логирования

		// формируем строку для добавления в файл лога
		var fstr string = vle.etime.Format("2006-01-02 15:04:05.000") + " p" + // дата, время
			RPads(strconv.FormatUint(vle.pid, 10), 8) + " " + // id обработки
			Vifs(vle.etype == 0, "Inf", "Err") + " " + // тип события
			RPads(filepath.Base(vle.gfile), 10) + " " + // имя go-файла
			fmt.Sprintf("%5d", vle.line) + " " +
			RPads(vle.funame, 15) + " " + // имя функции
			vle.estr + "\r\n" // текст события

		// записываем в log-файл
		if _, er := f.WriteString(fstr); er != nil {
			err = errors.New("Log file: write error: " + er.Error())
		}

		//fmt.Print("\nEvent has been writed");
	}
	defer f.Close()

	return err
}

// Vif -----------------------------------------------------------------------------
// if для типа interface{}
func Vif(result bool, v1 interface{}, v2 interface{}) (vv interface{}) {
	if result == true {
		vv = v1
	} else {
		vv = v2
	}
	return vv
}

// Vifs -----------------------------------------------------------------------------
// if для строк
func Vifs(result bool, v1 string, v2 string) (vv string) {
	if result == true {
		vv = v1
	} else {
		vv = v2
	}
	return vv
}

// StatToByte -----------------------------------------------------------------------------
// чтение файла в байт-массив
func StatToByte(assets http.FileSystem, fname string) ([]byte, error) {
	// input:
	// assets - файловая система в которой находится файл, который нужно поместить в http-ответ
	// fname  - имя сохраненного статического файла
	// output:
	// []byte - массив байт файла
	// error  - interface{} с ошибкой

	var fil []byte
	var err error

	pid := Getpid() // id обработки

	err = errors.New("OK\n")

	for true {
		// открываем файл
		file, er := assets.Open(fname)
		if er != nil {
			estr := "File open error"
			Vlogger.Vlog(pid, estr, 1)
			err = errors.New(estr)
			break
		}

		// получаем длину файла
		fstat, er := file.Stat()
		if er != nil {
			estr := "File stat getting error"
			Vlogger.Vlog(pid, estr, 1)
			err = errors.New(estr)
			break
		}

		// читаем файл в буфер
		fil = make([]byte, fstat.Size()) // создаем буфер для чтения файла: длина буфера = длина файла
		nx, er := file.Read(fil)
		if nx == 0 { // ошибка чтения файла
			estr := "File read error"
			Vlogger.Vlog(pid, estr, 1)
			err = errors.New(estr)
			break
		}

		defer file.Close() // безусловно закрываем файл в конце обработки
		Vlogger.Vlog(pid, fname+" was red", 0)
		break
	}

	return fil, err
}

// StatToHttp -----------------------------------------------------------------------------
// web server: статический файл -> http-ответ
func StatToHttp(w http.ResponseWriter, assets http.FileSystem, fname string, ftype string) error {
	// input:
	// w      - объект для http-ответа
	// assets - файловая система в которой находится файл, который нужно поместить в http-ответ
	// fname  - имя файла
	// ftype  - тип содержимого файла
	// output:
	// error  - interface{} с ошибкой

	var err error
	pid := Getpid() // id обработки

	for true {
		// открываем файл
		file, er := assets.Open(fname)
		if er != nil {
			estr := "File open error"
			Vlogger.Vlog(pid, estr, 1)
			errors.New(estr)
			break
		}

		// получаем длину файла
		fstat, er := file.Stat()
		if er != nil {
			estr := "File stat getting error"
			Vlogger.Vlog(pid, estr, 1)
			errors.New(estr)
			break
		}
		//fmt.Println("fname:", fname  )

		// создаем буфер для чтения всего файла
		buf := make([]byte, fstat.Size())
		nx, er := file.Read(buf)
		if nx == 0 {
			estr := "File read error"
			Vlogger.Vlog(pid, estr, 1)
			errors.New(estr)
			break
		}

		if ftype != "" {
			w.Header().Set("Content-Type", ftype)
		} // утанавливаем тип файла

		// пишем содержимое файла в http-ответ
		_, err = w.Write(buf)
		if err != nil {
			estr := "Sent to http error"
			Vlogger.Vlog(pid, estr, 1)
			errors.New(estr)
			break
		}

		// write the whole body at once
		//err = ioutil.WriteFile("_"+fname, buf, 0644)

		defer file.Close() // закрываем файл в конце обработки
		Vlogger.Vlog(pid, fname+" was sent to http-reply", 0)
		break
	}
	return err
}

// Vlog -----------------------------------------------------------------------------
// методы пакета
// -----------------------------------------------------------------------------
// Vlg Лог приложения
// -----------------------------------------------------------------------------
// запись строки в лог: вызывается из источника
func (vl *Vlg) Vlog(pid uint64, estr string, etype int) {
	// pid   - id обработки
	// estr  - произвольная строка
	// etype - тип события: 0-info, 1-error

	pc, file, line, _ := runtime.Caller(1)
	fn := runtime.FuncForPC(pc)

	var vle Vle
	vle.pid = pid
	vle.etime = time.Now()
	vle.line = line
	vle.funame = fn.Name()
	vle.estr = estr
	vle.etype = etype
	vle.gfile = file

	VcLog <- vle
}

// -----------------------------------------------------------------------------
func times(str string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(str, n)
}
func LPad(str string, length int, pad string) string { return times(pad, length-len(str)) + str } // left pad
func RPad(str string, length int, pad string) string { return str + times(pad, length-len(str)) } // right pad
func LPads(str string, length int) string            { return times(" ", length-len(str)) + str } // left spaces pad
func RPads(str string, length int) string            { return str + times(" ", length-len(str)) } // right spaces pad
//-----------------------------------------------------------------------------

// Qp параметры sql-запроса
type Qp struct {
	Qtxt string // текст запроса
	Rmax int    // макс. количество выбираемых записей
	Tmax int    // макс. длительность, секунд
}

// Qr результаты sql-запроса: произвольное количество записей
type Qr struct {
	Ar       [][]string        // массив записей, возвращенных запросом
	Nrows    int               // кол-во выбранных записей
	Ncols    int               // кол-во выбранных колонок
	Cols     []string          // список имен колонок
	ColTypes []*sql.ColumnType // список типов колонок
}

// Qr1 результаты sql-запроса: одна запись
type Qr1 struct {
	Ar       []string          // массив полей записи, возвращенной запросом
	Ncols    int               // кол-во выбранных колонок
	Cols     []string          // список имен колонок
	ColTypes []*sql.ColumnType // список типов колонок
}

// Qrx результаты sql-exec-запроса
type Qrx struct {
	sql.Result
	recs int
}

var Dba *sql.DB // дескриптор для коннекта к базе данных

// DbOpen -----------------------------------------------------------------------------
// определение дескриптора для коннекта к базе данных
func DbOpen(dbdescr string) error {
	// dbdescr - строка для коннекта к базе данных
	var err error
	pid := Getpid() // id обработки

	Dba, err = sql.Open("mysql", dbdescr)
	if err != nil {
		fmt.Println("DB open error:", err.Error())
		Vlogger.Vlog(pid, "DB open error", 1)
	}
	return err
}

// Qselect -----------------------------------------------------------------------------
// sql select: текст
func Qselect(qp Qp) (Qr, error) {
	var qr Qr
	pid := Getpid() // id обработки

	// выполняем запрос
	rows, err := Dba.Query(qp.Qtxt)
	if err != nil {
		Vlogger.Vlog(pid, "DB query error: "+err.Error()+": "+qp.Qtxt, 1)
		return qr, err
	}

	// заполняем поля результата
	qr.Cols, _ = rows.Columns()         // список имен колонок выборки
	qr.ColTypes, _ = rows.ColumnTypes() // cписок реквизитов колонок выборки
	qr.Ncols = len(qr.Cols)             // количество колонок
	qr.Nrows = 0                        // количество записей в выборке (начальное значение)

	// выбираем записи
	vals := make([]interface{}, qr.Ncols) // создаем массив для работы с очередной записью
	for i, _ := range qr.Cols {
		vals[i] = new(sql.RawBytes)
	} // инициализируем его типом sql.RawBytes

	qr.Ar = make([][]string, 0, 1000)

	for rows.Next() {
		err := rows.Scan(vals...) // считываем все колонки запроса в поля массива []interface{}
		if err != nil {
			break
		}

		row := make([]string, qr.Ncols) // массив для записи результата

		for i := 0; i < qr.Ncols; i++ { // переделываем значения полей очередной записи в тип string
			row[i] = string(*vals[i].(*sql.RawBytes))
		}

		qr.Ar = append(qr.Ar, row) // добавляем очередную запись к массиву результатов
		qr.Nrows++                 // счет считанных записей
		if qr.Nrows >= qp.Rmax {
			break
		} // проверяем на превышение max-предела числа выбранных записей

	}

	defer rows.Close()

	return qr, err
}

// Qrow -----------------------------------------------------------------------------
// sql select row: текст
func Qrow(qp Qp) (Qr1, error) {

	var qr1 Qr1
	qp.Rmax = 1
	qr, err := Qselect(qp)
	if err != nil {
		return qr1, err
	}

	qr1.Ar = qr.Ar[0]          // поля записи
	qr1.Ncols = qr.Ncols       // кол-во выбранных колонок
	qr1.Cols = qr.Cols         // список имен колонок
	qr1.ColTypes = qr.ColTypes // список типов колонок

	return qr1, err
}

// Qexe -----------------------------------------------------------------------------
// sql exe
func Qexe(sql string) (Qrx, error) {

	pid := Getpid() // id обработки

	var qrx Qrx
	var err error

	res, err := Dba.Exec(sql)

	if err != nil {
		Vlogger.Vlog(pid, "DB exec error: "+err.Error()+": "+sql, 1)
		return qrx, err
	}

	qrx.Result = res

	return qrx, err
}

// Tr -----------------------------------------------------------------------------
// значения полей для обновления таблицы
type Tr map[string]interface{}

// Qmodify -----------------------------------------------------------------------------
// sql insert/modify
func Qmodify() {

	vv := make(Tr)
	vv = vv

	vv["123"] = 123

}

// Sl -----------------------------------------------------------------------------
// строка в произвольном языке
type Sl map[string]string

// Dd DDIC Словарь Данных
type Dd struct {
	Name string        // имя базы данных
	Tit  Sl            // наименование базы данных
	Tbs  map[string]Tb // таблицы
	Des  map[string]De // элементы данных
	Dss  map[string]Ds // жесткие h-справочники
}

// Tb DDIC: таблица
type Tb struct {
	Name string        // имя таблицы
	Tit  Sl            // наименование таблицы
	Ord  int           // порядковый номер
	Ixs  map[string]Ix // индексы таблицы
	Fls  map[string]Fl // поля таблицы
}

// Fl DDIC: поле таблицы
type Fl struct {
	Tb   string // имя таблицы
	Name string // имя поля таблицы
	Tit  Sl     // наименование поля таблицы
	Ord  int    // порядковый номер

	Len  int    // длина поля таблицы
	Dec  int    // количество разрядов
	Type string // тип поля таблицы
	De   De     // элемент данных

	Dhn string // имя h-справочника
	Dtn string // имя таблицы-справочника
	Dfn string // имя поля таблицы-справочника
	Dpr string // дополнительные параметры проверки
}

// De DDIC: элемент данных
type De struct {
	Name string // имя элемента данных
	Tit  Sl     // наименование элемента данных
	Ord  int    // порядковый номер
}

// Ix DDIC: индекс таблицы
type Ix struct {
	Tb   string   // имя таблицы
	Name string   // имя индекса таблицы
	Ifs  []string // поля индекса таблицы
	Tit  Sl       // наименование индекса таблицы
	Ord  int      // порядковый номер
}

// Ds DDIC: жесткий h-справочник
type Ds struct {
	Name string   // имя h-справочника
	Tit  Sl       // наименование h-справочника
	Ord  int      // порядковый номер
	Rcs  []string // опции
}

var Dic Dd // словарь данных

// DdicLib -----------------------------------------------------------------------------
// библиотечный словарь: формирование
func DdicLib() {

	type dd []string

	Tbj := dd{`"BACKUPS", "DB Backups^Резервные копии баз данных^Деректер базасын Backup көшірмелері"`,

		`!"LK", "FILENAME", "FILESIZE"`,
		`!"Z1", "DBNAME",   "DBUSER",  "UNAME"`,
		`!"Z2", "PRIM",     "PID"`,

		`"VKEY",    0,  0, "int",      "=Ключ записи"`,
		`"DTB",     0,  0, "datetime", "=Дт.вр создания"`,
		`"CRTIM",   0,  0, "int",      "Duration of creation,sec^Время форм-я,сек^Қалыптастыру ұзақтығы,сек"`,
		`"DNAME",   "DBNAME"`,
	}

	//VS::$DD['BACKUPS'] = [LA('DB Backups^Резервные копии баз данных^Деректер базасын Backup көшірмелері'),
	//['LK'=>['FILENAME']

	//'VKEY'    =>[   0, 0, 'int'       , LC('Ключ записи')                                          ],
	//'DTB'     =>[   0, 0, 'datetime'  , LC('Дт,вр создания')                                       ],
	//'CRTIM'   =>[   0, 0, 'int'       , LA('Duration of creation,sec^Время форм-я,сек^Қалыптастыру ұзақтығы,сек')],

	//'DBNAME'  =>[  30, 0, 'varchar'   , LA('Db Name^Имя БД^Дерекқор атауы')                        ],
	//'DBUSER'  =>[  20, 0, 'varchar'   , LA('Db User^Имя пользователя БД^Дерекқор пайдаланушы аты') ],
	//'UNAME'   =>[  12, 0, 'varchar'   , LC('Имя пользователя')                                     ],
	//'FILENAME'=>[ 200, 0, 'varchar'   , LA('Name of BACKUP-file^Имя backup-файла^Сақтық көшірме файлын Аты')],
	//'FILESIZE'=>[   0, 0, 'int'       , LC('Размер файла')                                         ],
	//'PRIM'    =>[ 200, 0, 'varchar'   , LC('Примечание')                                           ],
	//'PID'     =>[  10, 0, 'varchar'   , 'PID'                                                      ]

	Tbj = Tbj

}

//-----------------------------------------------------------------------------
