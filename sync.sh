cd ..
rm -rf dapr_cn_bak
cp -r dapr_cn dapr_cn_bak
cd dapr_cn_bak
echo '[core]
        repositoryformatversion = 0
        filemode = true
        bare = false
        logallrefupdates = true
        ignorecase = true
        precomposeunicode = true
[pull]
        rebase = false
[remote "origin"]
        url = https://ls-2018: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@github.com/ls-2018/dapr_cn.git
        fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
        remote = origin
        merge = refs/heads/main
' > .git/config
git push --set-upstream origin master
cd ..
rm -rf dapr_cn_bak
