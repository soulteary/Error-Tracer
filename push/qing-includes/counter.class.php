<?php
/**
 * Error Tracer
 * 程序模版函数库。
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

class Counter extends Core
{
    private $args = array();
    private $process_time_start;
    private $process_time_end;

    function __construct()
    {
        $this->args = core::init_args(func_get_args());
        if ($this->args['DEBUG']) {
            $this->mktimestamp();
        }
        date_default_timezone_set('PRC');
        if ($this->args['GZIP'] && core::gzip_accepted()) {
            if (!ob_start(!$this->args['DEBUG'] ? 'ob_gzhandler' : null)) {
                ob_start();
            }
        }
        $this->result();
    }

    /**
     * 获取当前脚本运行时间
     *
     */
    protected function mktimestamp($end = false)
    {
        if (!$end) {
            $this->process_time_start = core::get_mircotime();
        } else {
            $this->process_time_end = core::get_mircotime();
            return number_format($this->process_time_end - $this->process_time_start, 5);
        }
    }

    private function result(){
        $DB = new MySql(array('MODE'=>'WRITE', 'DEBUG' => DEBUG));
        $ip = new IP(array('ONLYIP'=>true, 'ECHO'=>false));
        $DB_MSG = $DB->escapeSQL($_REQUEST['message']);
        $DB_URL = $DB->escapeSQL($_REQUEST['url']);
        $DB_LINE = $DB->escapeSQL($_REQUEST['line']);

        $browser = new Browser();
        $type = $browser->getBrowser();
        $version = $browser->getVersion();
        $platform =  $browser->getPlatform();
        $sql = "SELECT `id`, `type`, `version` FROM `browser` WHERE `type` = '$type' AND `version` = '$version' LIMIT 0, 1";
        $result = $DB->query($sql);
        $exec = $DB->num_rows($result);
        if($exec){
            $browser_list = $DB->fetch_array($result);
            $id = $browser_list['id'];
        }else{
            $sql = "INSERT INTO `browser` (`id`, `type`, `version`, `date`) VALUES (NULL, '$type', '$version', CURRENT_TIMESTAMP)";
            $DB->query($sql);
            $id = $DB->insert_id();
        }
        $sql= "INSERT INTO `test` (`id` ,`file` ,`line` ,`message` ,`browser` ,`status` ,`date`, `ip`,`platform`) VALUES (NULL , '$DB_URL', '$DB_LINE', '$DB_MSG', $id, 0, CURRENT_TIMESTAMP, ".ip2long($ip->result).",'$platform');";

        $exec = $DB->query($sql);

        if($exec){
            if ($this->args['DEBUG']) {

                    $error['title'] = "调试信息";
                    $message[] = "SQL: $sql";
                    $message[] = "SQL ERROR: $DB->geterror()";
                    $message[] = "REQUEST ARGU: $this->args";
                    $message[] = "IP: $ip->result";
                    $message[] = "DEBUG DATA: ".Debug::theDebug();
                    $message[] = "UA INFO: ".$_SERVER['HTTP_USER_AGENT'];
                    $message[] = "Process Time: ".$timestamp;
                    $error['message'] =$message;
                    Core::message($error);
            }
            else{
                header('Location: ../static/data.gif');
                exit();
            }
        }
        else{
            //若失败插入失败查询
            $sql= "INSERT INTO `error` (`id` ,`date`, `ip`) VALUES (NULL, CURRENT_TIMESTAMP, ".ip2long($ip->result).");";
            $DB->query($sql);
            header('Location: ../static/data.gif');
            exit();
        }
    }
}

?>