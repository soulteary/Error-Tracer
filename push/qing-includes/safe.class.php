<?php
/**
 * Error Tracer
 * 程序安全检查函数库。
 * @base Qing By soulteary
 * @version 1.0.0
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

abstract class Safe extends Core
{

    /**
     * 过滤请求中的非法字符
     *
     * @since 1.0.0
     *
     * @todo
     *      - 返回格式化。
     *
     * @return boolean 请求是否合法。
     */
    protected function validate()
    {
        $keyword = array("'", ";", "union", " ", "　", "%");
        $redirect = "";
        function is_exist($score, $keyword)
        {
            foreach ($keyword as $key => $value) {
                if (strstr($score, $value)) {
                    return true;
                }
            }
            return false;
        }

        $allvars = $_REQUEST;

        foreach ($allvars as $key => $value) {
            if (is_exist($value, $keyword)) {
                echo "<script language=\"javascript\">alert(\"感谢你的测试,如果有漏洞,不妨告诉我,谢谢!\");</script>";
                if (empty($redirect)) {
                    echo "<script language=\"javascript\">history.go(-1);</script>";
                } else {
                    echo "<script language=\"javascript\">window.location=\"" . $redirect . "\";</script>";
                }
                exit;
            }
        }
    }


    /**
     * 参数检查
     *
     * @since 1.0.0
     *
     * @todo
     *      - 待整理。
     *
     * @return boolean 请求是否合法。
     */
    protected function fir_check($q, $min, $max)
    {
        $var = isset($_REQUEST["$q"]) ? $_REQUEST["$q"] : "0";
        if (!is_numeric($var)) {
            return 0;
        } elseif (($max < $var) || ($min > $var)) {
            return 0;
        } else {
            return $var;
        }
    }

    /**
     * 参数检查
     *
     * @since 1.0.0
     *
     * @todo
     *      - 待整理。
     *
     * @return boolean 请求是否合法。
     */
    protected function fir_CRC_ok()
    {

        $str_crc = isset($_REQUEST["cr"]) ? $_REQUEST["cr"] : "0";
        $var = isset($_REQUEST["p"]) ? $_REQUEST["p"] : "0";
        $var2 = isset($_REQUEST["t"]) ? $_REQUEST["t"] : "0";
        $var3 = isset($_REQUEST["f"]) ? $_REQUEST["f"] : "0";

        if ($str_crc != "0") {

            $mainkey = $var . $var2 . $var3;
            $timekey = $_REQUEST["k"];

            $fir_code = $this->fir_crc_encode($mainkey, $timekey);

            if ($fir_code == $str_crc) {
                return true;
            } else {
                return false;
            }
        } else {
            return false;
        }

    }


    protected function fir_crc_encode($str_crc, $timekey)
    {
        $mainkey = array(1 => "1", 2 => "2", 3 => "3", 4 => "4", 5 => "5", 6 => "6",);
        $maskkey = array(1 => "Dp", 2 => "eF", 3 => "jw", 4 => "Kn", 5 => "iq", 6 => "oz",);
        $fir_cnt = str_replace($mainkey, $maskkey, $str_crc);
        $timekey = $this->fir_time_decode($timekey);
        $fir_cnt = base64_encode($fir_cnt . $timekey);
        $fir_cnt = substr($fir_cnt, 0, -2);
        $fir_cnt = strrev($fir_cnt);

        return $fir_cnt;
    }

    protected function fir_crc_decode($str_crc, $timekey)
    {
        $mainkey = array(1 => "1", 2 => "2", 3 => "3", 4 => "4", 5 => "5", 6 => "6",);
        $maskkey = array(1 => "Dp", 2 => "eF", 3 => "jw", 4 => "Kn", 5 => "iq", 6 => "oz",);
        $fir_cnt = strrev($str_crc);
        $fir_cnt .= "==";
        $fir_cnt = base64_decode($fir_cnt);
        $timekey = fir_time_decode($timekey);
        $fir_cnt = str_replace($timekey, "", $fir_cnt);
        $fir_cnt = str_replace($maskkey, $mainkey, $str_crc);

        return $fir_cnt;
    }


    /*----------------------------/
    /		   		错误警告            /
    ----------------------------*/
//
    protected function fir_waring()
    {
        echo '<strong>请勿修改参数。</strong>';
    }


    protected function fir_time_encode($str_time)
    {
        //检查时间是合格,省略
        $fir_key_num = array("0", "1", "2", "3", "4", "5", "6", "7", "8", "9");
        $fir_key_ltr = array("F", "i", "R", "e", "N", "d", "L", "S", "y", "u");
        $fir_code = '';
        $fir_code = str_replace($fir_key_num, $fir_key_ltr, $str_time);
        $fir_code = base64_encode($fir_code);
        $fir_code = substr($fir_code, 0, -2);
        $fir_code = strrev($fir_code);

        return $fir_code;
    }

    protected function fir_time_decode($str_time)
    {
        //检查时间是合格,省略

        $fir_key_num = array("0", "1", "2", "3", "4", "5", "6", "7", "8", "9");
        $fir_key_ltr = array("F", "i", "R", "e", "N", "d", "L", "S", "y", "u");
        $fir_code = '';
        $fir_code = strrev($str_time);
        $fir_code .= "==";
        $fir_code = base64_decode($fir_code);
        $fir_code = str_replace($fir_key_ltr, $fir_key_num, $fir_code);

        return $fir_code;
    }


}
