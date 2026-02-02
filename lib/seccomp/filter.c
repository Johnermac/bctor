#include <stddef.h>          // for offsetof
#include <linux/seccomp.h>   // for struct seccomp_data
#include <linux/filter.h>    // for struct sock_filter, BPF_STMT, BPF_JUMP, etc.
#include <linux/audit.h>     // for AUDIT_ARCH_X86_64
#include <sys/syscall.h>     // for SYS_read, SYS_write, etc.
#include "rules.h"           // your ALLOW_SYSCALL, KILL_PROCESS macros

struct sock_filter filter[] = {
    // arch check
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, arch)),
    BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, AUDIT_ARCH_X86_64, 1, 0),
    KILL_PROCESS,

    // syscall number
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr)),

    ALLOW_SYSCALL(execve),
    ALLOW_SYSCALL(brk),
    ALLOW_SYSCALL(mmap),
    ALLOW_SYSCALL(munmap),    // Liberar memória
    ALLOW_SYSCALL(mprotect),  // Proteção de memória (necessário para carregar bibliotecas)
    ALLOW_SYSCALL(access),
    ALLOW_SYSCALL(fstat),
    ALLOW_SYSCALL(newfstatat),// Versão moderna do fstat (muito comum em distros novas)
    ALLOW_SYSCALL(arch_prctl),
    ALLOW_SYSCALL(openat),    // Abrir arquivos (libc, libs)
    ALLOW_SYSCALL(close),     // Fechar arquivos
    ALLOW_SYSCALL(read),
    ALLOW_SYSCALL(write),
    ALLOW_SYSCALL(pread64),   // Leitura em offsets (carregamento de ELF)
    ALLOW_SYSCALL(set_tid_address), // Init de threads da libc    
    ALLOW_SYSCALL(rt_sigaction), // Sinais (Ctrl+C, etc)
    ALLOW_SYSCALL(getuid),    // Shell precisa saber quem é você
    ALLOW_SYSCALL(getgid),
    ALLOW_SYSCALL(getpid),    
    ALLOW_SYSCALL(fcntl),
    ALLOW_SYSCALL(prlimit64),  // A que causou o crash no strace
    ALLOW_SYSCALL(setgid),     // Comum em setups de privilégios
    ALLOW_SYSCALL(setuid),     // Comum em setups de privilégios
    ALLOW_SYSCALL(setgroups),  // Comum em setups de privilégios    
    ALLOW_SYSCALL(set_robust_list), // A que causou o crash (SYS_set_robust_list)
    
    ALLOW_SYSCALL(capget),         // Shells checam capacidades (SYS_capget)
    ALLOW_SYSCALL(capset),
    ALLOW_SYSCALL(set_robust_list),
    ALLOW_SYSCALL(rseq),           // A que causou o crash (SYS_rseq)
    
    ALLOW_SYSCALL(uname),          // A que causou o crash agora
    ALLOW_SYSCALL(stat),           // Checar arquivos
    ALLOW_SYSCALL(lstat),          // Checar links simbólicos
    ALLOW_SYSCALL(readlink),       // Ler caminhos de links
    
    ALLOW_SYSCALL(pipe),           // Shell usa pipes para comandos
    ALLOW_SYSCALL(pipe2),          // Versão moderna do pipe
    ALLOW_SYSCALL(wait4),          // Shell precisa esperar os comandos terminarem
    ALLOW_SYSCALL(getrandom),      // A que causou o crash (SYS_getrandom)
    ALLOW_SYSCALL(poll),           // Esperar por eventos de E/S
    ALLOW_SYSCALL(select),         // Alternativa ao poll
    ALLOW_SYSCALL(rt_sigprocmask), // Gerenciar bloqueio de sinais
    ALLOW_SYSCALL(sigaltstack),    // Stack de sinais para handlers
    ALLOW_SYSCALL(prctl),          // A que causou o crash (PR_SET_NAME)    
    ALLOW_SYSCALL(getpgrp),        // Shell precisa saber o grupo do processo
    ALLOW_SYSCALL(getppid),        // Saber quem é o pai
    ALLOW_SYSCALL(getcwd),         // Para mostrar o caminho no prompt (pwd)
    ALLOW_SYSCALL(setpgid),        // A que causou o crash (SYS_setpgid)    
    ALLOW_SYSCALL(rt_sigreturn),   // Retorno de sinal    
    ALLOW_SYSCALL(geteuid),        // A que causou o crash (SYS_geteuid)
    ALLOW_SYSCALL(getegid),        // Quase sempre chamada junto com geteuid
    ALLOW_SYSCALL(getresuid),      // Verificação detalhada de identidade
    ALLOW_SYSCALL(getresgid),
    ALLOW_SYSCALL(ioctl),          // Essencial: o log mostrou várias chamadas de controle de terminal (TCSETS, etc)
    ALLOW_SYSCALL(clone),          // A que causou o crash (essencial para rodar comandos)
    ALLOW_SYSCALL(getgroups),
    ALLOW_SYSCALL(getdents64),   // OBRIGATÓRIA para o ls listar arquivos
    ALLOW_SYSCALL(chdir),        // A syscall que o 'cd' usa de fato
    ALLOW_SYSCALL(dup),
    ALLOW_SYSCALL(dup2),
    ALLOW_SYSCALL(dup3),   
    /* Gerenciamento de Processos e Finalização */
    ALLOW_SYSCALL(exit_group),     // OBRIGATÓRIO para processos filhos terminarem
       
    /* Navegação e Sistema de Arquivos */
    ALLOW_SYSCALL(lseek),          // ps e id usam para ler arquivos de sistema/config
    ALLOW_SYSCALL(readlinkat),     // Algumas versões de ls/ps precisam
    ALLOW_SYSCALL(getdents),       // Algumas glibc antigas usam a versão 32-bit (SYS_getdents)
    ALLOW_SYSCALL(socket),         // id usa para nscd (Name Service Cache Daemon)
    ALLOW_SYSCALL(connect),        // id usa para nscd
    ALLOW_SYSCALL(futex),




    // …

    KILL_PROCESS,
};

unsigned int filter_len = sizeof(filter) / sizeof(filter[0]);