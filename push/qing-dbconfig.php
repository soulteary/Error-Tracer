<?php
/**
 * Error Tracer
 *
 * 数据库设置
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */
/** 数据库名称 */
define("DB_NAME", SAE_MYSQL_DB);
/** 数据库用户名称 */
define("DB_USER", SAE_MYSQL_USER);
/** 数据库用户名称 */
define("DB_PASS", SAE_MYSQL_PASS);
/** 主数据库 */
define("DB_HOST_WRITE", SAE_MYSQL_HOST_M.':'.SAE_MYSQL_PORT);
/** 从数据库 */
define("DB_HOST_READ", SAE_MYSQL_HOST_S.':'.SAE_MYSQL_PORT);
?>