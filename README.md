# check_docker_OOMkiller

## SYNOPSIS

```
$ check_docker_OOMkiller -l /tmp/check_docker_oomkiller
WARNING: Container 1da1ac35179e4a431244118025a4806f88ca172ffdba9e035076add06e912e6f (stress) was killed by OOM killer
exit status 1
```

## DESCRIPTION
this is plugin for nagios, icinga or compatible monitoring

this plugin list non-running containers and check which was killed by OOM killer (means container have less memory then need)

because I don't each run of this plugin report same OOMKilled containers, is possible save last checked container (id) to file

this plugin comunicate with docker API via `unix:///var/run/docker.sock` and must have right rights (root/docker-group)

### how it works?
this plugin list do `inspect` to all non-running container and check State.OOMKilled flag

this plugin do same like this command:
```
docker ps -a -q --filter=status=exited --filter=status=dead --filter=since=$LAST_CHECKED_CONTAINER_ID | xargs docker inspect --format "{{.State.OOMKilled}} {{.ID}} {{.Config.Image}}" | grep -E '^true'
```

## OPTIONS
* `-l` - file path to persist last checked container id
* `-w` - OOM killed container is report as warning (default)
* `-c` - OOM killed container is report as critical

## WHY EXISTS THIS PLUGIN?

of course is possible find OOM killer in /var/log/messages (dmesg)

```
Dec 17 00:34:31 fanatica kernel: stress invoked oom-killer: gfp_mask=0x24000c0(GFP_KERNEL), order=0, oom_score_adj=0
Dec 17 00:34:31 fanatica kernel: stress cpuset=docker-1da1ac35179e4a431244118025a4806f88ca172ffdba9e035076add06e912e6f.scope mems_allowed=0
Dec 17 00:34:31 fanatica kernel: CPU: 1 PID: 12906 Comm: stress Tainted: P           OE   4.8.11-200.fc24.x86_64 #1
Dec 17 00:34:31 fanatica kernel: Hardware name: Dell Inc. Precision T1600/06NWYK, BIOS A07 10/17/2011
Dec 17 00:34:31 fanatica kernel: 0000000000000286 00000000250c8593 ffffa232b5bb3c50 ffffffffbb3e5f4d
Dec 17 00:34:31 fanatica kernel: ffffa232b5bb3d30 ffffa23554033d00 ffffa232b5bb3cb8 ffffffffbb24c308
Dec 17 00:34:31 fanatica kernel: ffffa2359d219580 ffffa23474e29e80 ffffffffbb1bcd86 0000000000080000
Dec 17 00:34:31 fanatica kernel: Call Trace:
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb3e5f4d>] dump_stack+0x63/0x86
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb24c308>] dump_header+0x5c/0x1d5
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb1bcd86>] ? find_lock_task_mm+0x36/0x80
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb1bd97c>] oom_kill_process+0x20c/0x3d0
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb23ccb5>] ? mem_cgroup_iter+0x105/0x2d0
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb23f2be>] mem_cgroup_out_of_memory+0x2ce/0x310
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb24022b>] mem_cgroup_oom_synchronize+0x33b/0x350
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb23abb0>] ? get_mem_cgroup_from_mm+0xa0/0xa0
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb1be01c>] pagefault_out_of_memory+0x4c/0xc0
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb062274>] mm_fault_error+0x94/0x190
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb0627f4>] __do_page_fault+0x484/0x4d0
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb062870>] do_page_fault+0x30/0x80
Dec 17 00:34:31 fanatica kernel: [<ffffffffbb804dc8>] page_fault+0x28/0x30
Dec 17 00:34:31 fanatica kernel: Task in /system.slice/docker-1da1ac35179e4a431244118025a4806f88ca172ffdba9e035076add06e912e6f.scope killed as a result of limit of /system.slice/docker-1da1ac35179e4a431244118025a4806f88ca172ffdba9e035076add06e912e6f.scope
Dec 17 00:34:31 fanatica kernel: memory: usage 1048576kB, limit 1048576kB, failcnt 70428
Dec 17 00:34:31 fanatica kernel: memory+swap: usage 2097080kB, limit 2097152kB, failcnt 17
Dec 17 00:34:31 fanatica kernel: kmem: usage 4708kB, limit 9007199254740988kB, failcnt 0
Dec 17 00:34:31 fanatica kernel: Memory cgroup stats for /system.slice/docker-1da1ac35179e4a431244118025a4806f88ca172ffdba9e035076add06e912e6f.scope: cache:0KB rss:1043868KB rss_huge:0KB mapped_file:0KB dirty:0KB writeback:82044KB swap:1048504KB inactive_anon:521952KB active_anon:521912KB inactive_file:0KB active_file:0KB unevictable:0KB
Dec 17 00:34:31 fanatica kernel: [ pid ]   uid  tgid total_vm      rss nr_ptes nr_pmds swapents oom_score_adj name
Dec 17 00:34:31 fanatica kernel: [12870]     0 12870     1866        0       9       3       23             0 stress
Dec 17 00:34:31 fanatica kernel: [12906]     0 12906   526155   239002    1031       5   284061             0 stress
Dec 17 00:34:31 fanatica kernel: Memory cgroup out of memory: Kill process 12906 (stress) score 999 or sacrifice child
Dec 17 00:34:31 fanatica kernel: Killed process 12906 (stress) total-vm:2104620kB, anon-rss:956008kB, file-rss:0kB, shmem-rss:0kB
```

but you see only name of application in docker (for java application you see `java invoked oom-killer: ...`)

if you can pair with container, you find docker id (for example `cpuset=docker-<ID>.scope`) and then run `docker inspect $ID`

this plugin do this without the need parsing /var/log/messages file (and without parsing related problems like logrotate, format, localization, ...)
