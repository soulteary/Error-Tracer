<?php
/**
 * Error Tracer
 * 数据库操作类
 * @base Qing By soulteary
 * @version 1.0.0
 * @include
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 * @todo
 *      将显示错误返回为JSON
 */

/**
 * MYSQL数据操方法封装类
 */
if (!defined('FILE_PREFIX')) die('Silence is golden.');

class MySQL extends Core
{
    private $connect;
    private $args = array();

    /**
     * 查询次数
     * @var int
     */
    private $queryCount = 0;


    /**
     * 内部数据结果
     * @var resourse
     */
    private $result;

    /**
     * 构造函数
     */
    function __construct()
    {
        $this->args = core::init_args(func_get_args());

        if (!function_exists('mysql_connect')) {
            $error = "服务器PHP不支持MySQL数据库。";
            if ($this->args['DEBUG']) {
                Core::message("<p>$error</p>");
            }else{
                Core::json($error);
            }
        }

        if($this->args['MODE']=="WRITE"){
            $SELECT_DB = DB_HOST_WRITE;
        }else{
            $SELECT_DB = DB_HOST_READ;
        }

        if (!$this->connect = mysql_connect($SELECT_DB, DB_USER, DB_PASS)) {
            $error = "连接数据库失败，可能是数据库用户名或密码错误。";
            if ($this->args['DEBUG']) {
                Core::message("<p>$error</p>");
            }else{
                Core::json($error);
            }
        }

        $connect = mysql_select_db(DB_NAME, $this->connect);
        if (!$connect) {
            $error = "未找到指定数据库。";
            if ($this->args['DEBUG']) {
                Core::message("<p>$error</p>");
            }else{
                Core::json($error);
            }
        }
    }

    /**
     * 关闭数据库连接
     */
    public function close()
    {
        return mysql_close($this->conn);
    }

    /**
     * 发送查询语句
     *
     */
    public function query($sql)
    {
        $this->result = mysql_query($sql, $this->connect);
        $this->queryCount++;

        if (!$this->result) {
            $error['title'] = "SQL语句执行错误";
            if ($this->args['DEBUG']) {
                $message[] = $sql;
                $message[] = $this->geterror();
                $error['message'] =$message;
                Core::message($error);
            }else{
                $error['message'] = $sql;
                $error['error'] = $this->geterror();
                Core::json($error);
            }
        }else {
            return $this->result;
        }
    }


    public function escapeSQL($str){
        return mysql_real_escape_string($str, $this->connect);
    }

    /**
     * 从结果集中取得一行作为关联数组/数字索引数组
     *
     */
    public function fetch_array($query, $type = MYSQL_ASSOC)
    {
        return mysql_fetch_array($query, $type);
    }

    public function once_fetch_array($sql)
    {
        $this->result = $this->query($sql);
        return $this->fetch_array($this->result);
    }

    /**
     * 从结果集中取得一行作为数字索引数组
     *
     */
    public function fetch_row($query)
    {
        return mysql_fetch_row($query);
    }

    /**
     * 取得行的数目
     *
     */
    function num_rows($query)
    {
        return mysql_num_rows($query);
    }

    /**
     * 取得结果集中字段的数目
     */
    function num_fields($query)
    {
        return mysql_num_fields($query);
    }

    /**
     * 取得上一步 INSERT 操作产生的 ID
     */
    function insert_id()
    {
        return mysql_insert_id($this->conn);
    }

    /**
     * 获取mysql错误
     */
    function geterror()
    {
        return mysql_error();
    }

    /**
     * Get number of affected rows in previous MySQL operation
     */
    function affected_rows()
    {
        return mysql_affected_rows();
    }

    /**
     * 取得数据库版本信息
     */
    function getMysqlVersion()
    {
        return mysql_get_server_info();
    }

    /**
     * 取得数据库查询次数
     */
    function getQueryCount()
    {
        return $this->queryCount;
    }
}
