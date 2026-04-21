import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { SourceAssociationNamesResponseCommandOptions } from './options';
import { SourceAssociationNamesResponseCommandParams } from './params';

/**
 * Source Association Names Response Message Validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<SourceAssociationNamesResponseCommandParams>}
 */
export class CommandParamsValidator implements IValidator<SourceAssociationNamesResponseCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {SourceAssociationNamesResponseCommandParams} data the command parameters
     * @param {SourceAssociationNamesResponseCommandOptions} _options the command options (internally used)
     * @memberof CommandParamsValidator
     */
    constructor(
        readonly data: SourceAssociationNamesResponseCommandParams,
        private readonly _options: SourceAssociationNamesResponseCommandOptions
    ) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isFirstSourceIdOutOfRange()) {
            errors.firstSourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isFirstSourceIdAndMaximumNumberOfNamesOutOfRange()) {
            errors.SourceIdAndMaximumNumberOfNames = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ASSOCIATION_NAME_ID_AND_MAXIMUM_NUMBER_OF_SOURCE_ASSOCIATION_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceNamesItemsOutOfRange()) {
            errors.sourceAssociationNameItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if matrixId is out of range
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the firstSourceId is out of range
     * + false = firstSourceId is included within the limits
     * + true = firstSourceId [0-65535] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isFirstSourceIdOutOfRange(): boolean {
        return this.data.firstSourceId < 0 || this.data.firstSourceId > 65535;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isFirstSourceIdAndMaximumNumberOfNamesOutOfRange(): boolean {
        return (
            this.data.firstSourceId + this.data.numberOfSourceAssociationNamesTofollow < 0 ||
            this.data.firstSourceId + this.data.numberOfSourceAssociationNamesTofollow > 65535
        );
    }

    /**
     * Gets a boolean indicating whether the source Association Name Items is out of range
     * + false = sourceAssociationNameItems is included within the limits
     * + true = sourceAssociationNameItems  [depend of the lengthOfNames.byteLength] is out of range
     * @private
     * @returns {boolean} indicating if Source Association Names Items length is out of range
     * @memberof CommandParamsValidator
     */
    private isSourceNamesItemsOutOfRange(): boolean {
        if (this.data.sourceAssociationNameItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.sourceAssociationNameItems.length; byteIndex++) {
            const data = this.data.sourceAssociationNameItems[byteIndex];
            if (data.length < 1 || data.length > this._options.lengthOfNames.byteLength) {
                return true;
            }
        }
        return false;
    }
}
