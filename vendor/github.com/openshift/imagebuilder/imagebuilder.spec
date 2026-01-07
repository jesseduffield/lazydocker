
#debuginfo not supported with Go
%global debug_package %{nil}

# modifying the Go binaries breaks the DWARF debugging
%global __os_install_post %{_rpmconfigdir}/brp-compress

%{!?commit: %global commit HEAD }

#
# Customize from here.
#

%global golang_version 1.19
%{!?version: %global version 1.2.19}
%{!?release: %global release 1}
%global package_name imagebuilder
%global product_name Container Image Builder
%global import_path github.com/openshift/imagebuilder

Name:           %{package_name}
Version:        %{version}
Release:        %{release}%{?dist}
Summary:        Builds Dockerfile using the Docker client
License:        ASL 2.0
URL:            https://%{import_path}

Source0:        https://%{import_path}/archive/%{commit}/%{name}-%{version}.tar.gz
BuildRequires:  golang >= %{golang_version}

### AUTO-BUNDLED-GEN-ENTRY-POINT

%description
Builds Dockerfile using the Docker client

%prep
GOPATH=$RPM_BUILD_DIR/go
rm -rf $GOPATH
mkdir -p $GOPATH/{src/github.com/openshift,bin,pkg}
%setup -q -c -n imagebuilder
cd ..
mv imagebuilder $GOPATH/src/github.com/openshift/imagebuilder
ln -s $GOPATH/src/github.com/openshift/imagebuilder imagebuilder

%build
export GOPATH=$RPM_BUILD_DIR/go
cd $GOPATH/src/github.com/openshift/imagebuilder
go install ./cmd/imagebuilder

%install

install -d %{buildroot}%{_bindir}
install -p -m 755 $RPM_BUILD_DIR/go/bin/imagebuilder %{buildroot}%{_bindir}/imagebuilder

%files
%doc README.md
%license LICENSE
%{_bindir}/imagebuilder

%pre

%changelog

