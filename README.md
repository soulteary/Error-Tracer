Error-Tracer (@soulteary)
============

简单的前端问题追踪程序，完善空间很大。


DEMO地址:
http://errorboard.sinaapp.com/

DEMO后台:
http://errorboard.sinaapp.com/push/?mode=admin


数据库结构:

	CREATE TABLE `browser` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `type` varchar(20) NOT NULL,
	 `version` varchar(20) NOT NULL,
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=27 DEFAULT CHARSET=utf8

	CREATE TABLE `error` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '错误时间',
	 `ip` decimal(11,0) NOT NULL,
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=171 DEFAULT CHARSET=utf8
	
	CREATE TABLE `test` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `file` text NOT NULL COMMENT '报错脚本路径',
	 `line` mediumint(8) unsigned NOT NULL COMMENT '报错脚本行号',
	 `message` text NOT NULL COMMENT '报错信息',
	 `browser` smallint(5) unsigned NOT NULL COMMENT '浏览器类型号',
	 `status` tinyint(3) unsigned NOT NULL DEFAULT '0' COMMENT '错误状态',
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '提交时间',
	 `ip` decimal(11,0) NOT NULL,
	 `platform` varchar(15) NOT NULL DEFAULT 'unknown' COMMENT '操作系统',
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=271 DEFAULT CHARSET=utf8