/**
 * Provides the properties of the current package as read from the 'package,json' file
 *
 * @export
 * @class Package
 */
export class Package {
    private packageInfo: any;

    /**
     * Creates an instance of Package
     *
     * @param {string} packageJsonPath the path to the package.json file
     * @memberof Package
     */
    constructor(private packageJsonPath: string) {
        this.packageInfo = this.loadPackageInfo(packageJsonPath);
    }

    /**
     * Gets the package version
     *
     * @readonly
     * @type {string}
     * @memberof Package
     */
    get version(): string {
        return this.packageInfo.version;
    }

    /**
     * Gets the package name
     *
     * @readonly
     * @type {string}
     * @memberof Package
     */
    get name(): string {
        return this.packageInfo.name;
    }

    /**
     * Load the 'package.json' file
     *
     * @private
     * @param {string} packageJsonPath
     * @returns {*}
     * @memberof Package
     */
    private loadPackageInfo(packageJsonPath: string): any {
        return require(packageJsonPath);
    }
}
