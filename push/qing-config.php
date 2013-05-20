<?php
/**
 * Error Tracer
 *
 * 程序基础设置文件
 * 文件包含以下内容：文件版本、调试模式开关、主题选择、GZIP压缩开关、语言设置、字符集设置。
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

/** 程序版本 */
define("VERSION", "1.0.0");
/** 调试模式开关 */
define('DEBUG', false);
/** 开启GZIP压缩(如果调试模式被激活，那么忽略此设置。) */
define('GZIP', true);
/** 语言设置 */
define('Q_LANG', 'zh-CN');
/** 字符集设置 */
define('Q_CHARSET', 'UTF-8');

/** 数据库配置 */
if(file_exists(FILE_PREFIX."dbconfig.php")){
    include(FILE_PREFIX."dbconfig.php");
}

/** 载入程序模块 */
function __autoload($classname)
{
    $fileName = ABSPATH . FILE_PREFIX . "includes/" . strtolower($classname);
    $classFile = $fileName . ".class.php";
    $libFile = $fileName . ".lib.php";

    if (file_exists($libFile)) {
        include($libFile);
    }
    if (file_exists($classFile)) {
        include($classFile);
    }
}


if(isset($_REQUEST["mode"]) && !empty($_REQUEST["mode"]))
{
    $EB = new Admin(array('GZIP' => GZIP));
}else{
    /** 计数器更新 */
    $EB = new Counter(array('GZIP' => GZIP, 'DEBUG' => DEBUG));
}

?>