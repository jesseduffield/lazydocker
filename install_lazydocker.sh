#!/bin/bash

go install github.com/jesseduffield/lazydocker@latest


# Baixa e instala o lazydocker
curl https://raw.githubusercontent.com/jesseduffield/lazydocker/master/scripts/install_update_linux.sh | bash

# Adiciona $HOME/go/bin ao PATH no bashrc se ainda não existir
if ! grep -q 'export PATH="$HOME/go/bin:$PATH"' ~/.bashrc; then
  echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc
  echo '[+] Adicionado $HOME/go/bin ao PATH no ~/.bashrc'
fi

echo -e "\n✅ Instalação concluída!"
echo "⚠️  Execute: source ~/.bashrc"
echo "📦 Depois, rode: lazydocker"
