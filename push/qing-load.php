<?php
/**
 * Error Tracer
 *
 * 程序加载器。
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

define( 'ABSPATH', dirname(__FILE__) . '/' );

error_reporting( E_CORE_ERROR | E_CORE_WARNING | E_COMPILE_ERROR | E_ERROR | E_WARNING | E_PARSE | E_USER_ERROR | E_USER_WARNING | E_RECOVERABLE_ERROR );

if ( file_exists( ABSPATH . FILE_PREFIX .'config.php') ) {
    require_once( ABSPATH . FILE_PREFIX . 'config.php' );
}else{
    die('No more.');
}
?>