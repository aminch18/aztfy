package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

func main() {
	pkgs, err := loadPackage("/home/magodo/projects/terraform-provider-azurerm", []string{"./internal/sdk", "./internal/services/web"})
	if err != nil {
		log.Fatal(err)
	}

	sdkPkg := pkgs[0].GoPackage
	var typedRegistration, untypedRegistration *types.Interface
	for _, obj := range sdkPkg.TypesInfo.Defs {
		if obj == nil {
			continue
		}
		t := obj.Type()
		nt, ok := t.(*types.Named)
		if !ok {
			continue
		}
		ut, ok := nt.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		switch obj.Name() {
		case "TypedServiceRegistration":
			typedRegistration = ut
		case "UntypedServiceRegistration":
			untypedRegistration = ut
		}
	}
	if typedRegistration == nil {
		log.Fatal("can't find TypedServiceRegistration")
	}
	if untypedRegistration == nil {
		log.Fatal("can't find UntypedServiceRegistration")
	}

	webPkg := pkgs[1]

	for _, obj := range webPkg.GoPackage.TypesInfo.Defs {
		if obj == nil || obj.Name() != "Registration" {
			continue
		}
		if types.Implements(obj.Type(), typedRegistration) {
			if _, err := handleTypedRegistration(webPkg, obj); err != nil {
				log.Fatal(err)
			}
		}
		if types.Implements(obj.Type(), untypedRegistration) {
			if _, err := handleUntypedRegistration(webPkg, obj); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func handleTypedRegistration(pkg *Package, obj types.Object) (map[string]string, error) {
	// TF resource type -> Azure api path
	resourceMapping := map[string]string{}

	// Iterate each sdk.Resource as defined in the Resources() method.
	nt, ok := obj.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s is not a named type", obj.Type())
	}
	f, err := functionDeclOfMethod(pkg.GoPackage, nt, "Resources")
	if err != nil {
		return nil, err
	}
	retStmt, ok := f.Body.List[0].(*ast.ReturnStmt)
	if !ok {
		return nil, fmt.Errorf("the first statement of the function %s is not a return statement", pkg.GoPackage.Fset.Position(f.Pos()))
	}

	result, ok := retStmt.Results[0].(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("the returned expression of the function %s is not a composite literal", pkg.GoPackage.Fset.Position(f.Pos()))
	}

	for _, resExpr := range result.Elts {
		resComplit, ok := resExpr.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("the returned resource %s is not a composite literal", pkg.GoPackage.Fset.Position(resExpr.Pos()))
		}
		resTypeObj, ok := pkg.GoPackage.TypesInfo.Defs[resComplit.Type.(*ast.Ident)]
		if !ok {
			return nil, fmt.Errorf("failed to find the type definition for %s", pkg.GoPackage.Fset.Position(resExpr.Pos()))
		}

		tfName, apiPath, err := handleTypedResource(pkg, resTypeObj)
		if err != nil {
			return nil, err
		}
		resourceMapping[tfName] = apiPath

	}

	return resourceMapping, nil
}

func handleTypedResource(pkg *Package, obj types.Object) (string, string, error) {
	// Identify the TF resource type.
	// The TF resource type is defined in its ResourceType() method
	f, err := functionDeclOfMethod(pkg.GoPackage, obj.Type().(*types.Named), "ResourceType")
	if err != nil {
		return "", "", err
	}
	retStmt, ok := f.Body.List[0].(*ast.ReturnStmt)
	if !ok {
		return "", "", fmt.Errorf("the first statement of the function %s is not a return statement", pkg.GoPackage.Fset.Position(f.Pos()))
	}
	result, ok := retStmt.Results[0].(*ast.BasicLit)
	if !ok {
		return "", "", fmt.Errorf("the returned expression of the function %s is not a basic literal", pkg.GoPackage.Fset.Position(f.Pos()))
	}
	tfResourceType, err := strconv.Unquote(result.Value)
	if err != nil {
		return "", "", err
	}

	// Identify the Azure API path.
	// The API path comes from its Delete() method

	return tfResourceType, "", errors.New("TODO")
}

func handleUntypedRegistration(pkg *Package, obj types.Object) (map[string]string, error) {
	// TF resource type -> Azure api path
	resourceMapping := map[string]string{}
	return resourceMapping, nil
}

func functionDeclOfMethod(pkg *packages.Package, nt *types.Named, methodName string) (*ast.FuncDecl, error) {
	fileMap := map[*token.File]*ast.File{}
	for _, f := range pkg.Syntax {
		fileMap[pkg.Fset.File(f.Pos())] = f
	}

	for i := 0; i < nt.NumMethods(); i++ {
		method := nt.Method(i)
		if method.Name() != methodName {
			continue
		}

		f := fileMap[pkg.Fset.File(method.Pos())]
		// Lookup the function declaration from the method identifier position.
		// The returned enclosing interval starts from the identifier node, then the function declaration node.
		nodes, _ := astutil.PathEnclosingInterval(f, method.Pos(), method.Pos())
		fdecl := nodes[1].(*ast.FuncDecl)
		return fdecl, nil
	}

	return nil, fmt.Errorf("failed to find the method %q in type %q", methodName, nt.Obj().Name())
}
