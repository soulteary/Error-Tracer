<?php
/**
 * Error Tracer
 *
 * 程序 IP&GEO 函数库。
 * 获取访客的IP信息以及IP对应的位置(SINA NSLOOKUP 接口)。
 * @base Qing By soulteary
 * @version 1.0.0
 * @include
 *          - @function GetIP                       获取IP信息
 * @eg
 *      new IP(array('ONLYIP'=>true, 'ECHO'=>true));
 *      new IP(array('FORMAT'=>'JSON','ECHO'=>true));
 *
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

class IP
{
    private $args = array();
    public $result = false;

    function __construct()
    {
        $this->args = core::init_args(func_get_args());
        $ip = self::GetIP();

        $ret = preg_match_all('/(\d+\.){3}\d+/i', $ip, $result);
        if (!$ret) {
            return false;
        } else {
            $result = implode(array_unique($result[0]));
        }

        if (isset($this->args['ONLYIP']) && $this->args['ONLYIP'] == true) {

            if (isset($this->args['FORMAT']) && $this->args['FORMAT'] == 'JSON') {
                $this->result = json_encode($result);
            } else {
                $this->result = $result;
            }
            if (isset($this->args['ECHO']) && $this->args['ECHO'] == true) {
                echo $this->result;
                return true;
            } else {
                return $this->result;
            }
        }
        else {

            $apiURL = 'http://int.dpool.sina.com.cn/iplookup/iplookup.php?ip=' . $result[0];
            if (isset($this->args['FORMAT']) && $this->args['FORMAT'] == 'JSON') {
                $apiURL .= '&format=json';
                $this->result = $this->ipCURL($apiURL);
            } else {
                $return = $this->ipCURL($apiURL);
                $this->result = iconv("GBK//IGNORE", "UTF-8", $return);
            }

            if (isset($this->args['ECHO']) && $this->args['ECHO'] == true) {
                echo $this->result;
                return true;
            } else {
                return $this->result;
            }

        }

    }

    /**
     * 获取IP地址
     *
     * @since 1.0.0
     *
     * @todo
     *      - 格式化最后的输出内容。
     * @eg. ip::GetIP();
     * @return string IP地址。
     */
    public function GetIP()
    {
        if (isset($_SERVER['HTTP_X_FORWARDED_FOR']) && $_SERVER['HTTP_X_FORWARDED_FOR'] && strcasecmp($_SERVER['HTTP_X_FORWARDED_FOR'], 'unknown')) {
            return $_SERVER['HTTP_X_FORWARDED_FOR'];
        } elseif (isset($_SERVER['REMOTE_ADDR']) && $_SERVER['REMOTE_ADDR'] && strcasecmp($_SERVER['REMOTE_ADDR'], 'unknown')) {
            return $_SERVER['REMOTE_ADDR'];
        }
    }

    /**
     * CURL 工具函数
     *
     * @since 1.0.0
     *
     * @params string $url 要请求的URL。
     * @return string $result 返回的结果。
     */
    private function ipCURL($url)
    {
        $ch = curl_init();
        curl_setopt($ch, CURLOPT_URL, $url);
        curl_setopt($ch, CURLOPT_HEADER, 0);
        ob_start();
        curl_exec($ch);
        curl_close($ch);
        $result = ob_get_contents();
        ob_end_clean();
        return $result;
    }
}

?>