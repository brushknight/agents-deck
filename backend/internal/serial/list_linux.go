package serial

func list() ([]Info, error) { return listSysfs("/sys") }
