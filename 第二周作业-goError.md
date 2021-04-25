**作业：我们在数据库操作的时候，比如 dao 层中当遇到一个 sql.ErrNoRows 的时候，是否应该 Wrap 这个 error，抛给上层。为什么，应该怎么做请写出代码？**



回答：

- 遇到sql.ErrNoRows，不用Wrap，直接提示“没有查到这行数据”

- 如果遇到非sql.ErrNoRows错误，则应该Wrap给上层，代码实现：

  ```
  var name string
  err = db.QueryRow("select name from users where id = ?", 1).Scan(&name)
  if err != nil {
  	if err == sql.ErrNoRows {
  		return name, nil
  	} else {
  		return name, errors.Wrap(err, "Query Row failed")
  	}
  }
  
  ```

  







2021年4月25日22:13:01