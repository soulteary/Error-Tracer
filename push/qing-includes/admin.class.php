<?php
/**
 * Error Tracer
 * 管理后台。
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

class Admin extends Core
{
    private $args = array();
    private $process_time_start;
    private $process_time_end;

    function __construct()
    {
        $this->args = core::init_args(func_get_args());
        $this->mktimestamp();
        date_default_timezone_set('PRC');
        if ($this->args['GZIP'] && core::gzip_accepted()) {
            if (!ob_start(!$this->args['DEBUG'] ? 'ob_gzhandler' : null)) {
                ob_start();
            }
        }
        if(isset($_REQUEST['a']) && !empty($_REQUEST['a'])){
            switch($_REQUEST['a']){
                case 'query':
                    $this->query();
                    break;
                case 'delete':
                    break;
            }
        }else{
            $this->index();
        }

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

    private function query(){
        $data = array();
        $DB = new MySql(array('MODE'=>'READ', 'DEBUG' => true));
        $DB2 = new MySql(array('MODE'=>'READ', 'DEBUG' => true));
        $sql = "SELECT * FROM `test` WHERE `status` = 0";
        $result = $DB->query($sql);
        $count = $DB->num_rows($result);
        $post = array();
        if($count){
            while ($item = $DB->fetch_array($result)) {
                $sql2 = "SELECT `type`, `version` FROM `browser` WHERE `id` = ".$item['browser']." LIMIT 0, 1";
                $result2 = $DB->query($sql2);
                $count2 = $DB->num_rows($result2);
                if($count2){
                    $browser_detail = $DB2->fetch_array($result2);
                }else{
                    $browser_detail = array('type'=>'unknown', 'version'=>'unknown');
                }
                array_push($post, array('id' => $item['id'], 'file' => $item['file'], 'line' => $item['line'], 'message' => $item['message'], 'date' => $item['date'], 'browser' => $browser_detail, 'status' => $item['status'], 'ip' => long2ip($item['ip']), 'platform' => $item['platform'], 'hash'=>md5($item['message'].$item['line'])));
            }

            $ip = new IP(array('ONLYIP'=>true, 'ECHO'=>false));
            $data['admin']['cost'] = $this->mktimestamp(true);
            $data['admin']['ip'] = $ip->result;
            $data['data'] = $post;
            Core::json($data);
        }else{
            $data['nodata'] = '暂无错误记录。';
            Core::json($data);
        }

    }

    private function index(){
        $tpl = file_get_contents(FILE_PREFIX.'content/'.FILE_PREFIX.'theme/index.html');
        echo $tpl;
    }
}

?>