import { BootstrapService } from '../../../../src/bootstrap.service';
import { SourceNamesResponseCommandParams } from '../../../../src/command/tx/106-source-names-response-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/106-source-names-response-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { SourceNamesResponseCommandOptions } from '../../../../src/command/tx/106-source-names-response-message/options';
import { NameLength } from '../../../../src/command/shared/name-length';

describe('Source Names Response Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: [
                    '0000',
                    '0001',
                    '0002',
                    '0003',
                    '0004',
                    '0005',
                    '0006',
                    '0007',
                    '0008',
                    '0009',
                    '0010',
                    '0011',
                    '0012',
                    '0013',
                    '0014',
                    '0015',
                    '0016',
                    '0017',
                    '0018',
                    '0019',
                    '0020',
                    '0021',
                    '0022',
                    '0023',
                    '0024',
                    '0025',
                    '0026',
                    '0027',
                    '0028',
                    '0029',
                    '0030',
                    '0031',
                    '0032',
                    '0033',
                    '0034',
                    '0035',
                    '0036',
                    '0037',
                    '0038',
                    '0039',
                    '0040',
                    '0041',
                    '0042',
                    '0043',
                    '0044',
                    '0045',
                    '0046',
                    '0047',
                    '0048',
                    '0049',
                    '0050',
                    '0051',
                    '0052',
                    '0053',
                    '0054',
                    '0055',
                    '0056',
                    '0057',
                    '0058',
                    '0059',
                    '0060',
                    '0061',
                    '0062',
                    '0063'
                ]
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
            };
            // Act
            const validator = new CommandParamsValidator(params, options);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: [
                    '0000',
                    '0001',
                    '0002',
                    '0003',
                    '0004',
                    '0005',
                    '0006',
                    '0007',
                    '0008',
                    '0009',
                    '0010',
                    '0011',
                    '0012',
                    '0013',
                    '0014',
                    '0015',
                    '0016',
                    '0017',
                    '0018',
                    '0019',
                    '0020',
                    '0021',
                    '0022',
                    '0023',
                    '0024',
                    '0025',
                    '0026',
                    '0027',
                    '0028',
                    '0029',
                    '0030',
                    '0031',
                    '0032',
                    '0033',
                    '0034',
                    '0035',
                    '0036',
                    '0037',
                    '0038',
                    '0039',
                    '0040',
                    '0041',
                    '0042',
                    '0043',
                    '0044',
                    '0045',
                    '0046',
                    '0047',
                    '0048',
                    '0049',
                    '0050',
                    '0051',
                    '0052',
                    '0053',
                    '0054',
                    '0055',
                    '0056',
                    '0057',
                    '0058',
                    '0059',
                    '0060',
                    '0061',
                    '0062',
                    '0063'
                ]
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
            };

            // Act
            const validator = new CommandParamsValidator(params, options);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstSourceId: -1,
                numberOfSourceNamesToFollow: 0,
                sourceNameItems: []
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
            };

            // Act
            const validator = new CommandParamsValidator(params, options);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.firstSourceId).toBeDefined();
            expect(errors.firstSourceId.id).toBe(CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdAndMaximumNumberOfNames).toBeDefined();
            expect(errors.sourceIdAndMaximumNumberOfNames.id).toBe(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceNamesItems).toBeDefined();
            expect(errors.sourceNamesItems.id).toBe(CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstSourceId: 65536,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: [
                    '0000',
                    '0001',
                    '0002',
                    '0003',
                    '0004',
                    '0005',
                    '0006',
                    '0007',
                    '0008',
                    '0009',
                    '0010',
                    '0011',
                    '0012',
                    '0013',
                    '0014',
                    '0015',
                    '0016',
                    '0017',
                    '0018',
                    '0019',
                    '0020',
                    '0021',
                    '0022',
                    '0023',
                    '0024',
                    '0025',
                    '0026',
                    '0027',
                    '0028',
                    '0029',
                    '0030',
                    '0031',
                    '0032',
                    '0033',
                    '0034',
                    '0035',
                    '0036',
                    '0037',
                    '0038',
                    '0039',
                    '0040',
                    '0041',
                    '0042',
                    '0043',
                    '0044',
                    '0045',
                    '0046',
                    '0047',
                    '0048',
                    '0049',
                    '0050',
                    '0051',
                    '0052',
                    '0053',
                    '0054',
                    '0055',
                    '0056',
                    '0057',
                    '0058',
                    '0059',
                    '0060',
                    '0061',
                    '0062',
                    '00630'
                ]
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
            };

            // Act
            const validator = new CommandParamsValidator(params, options);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.firstSourceId).toBeDefined();
            expect(errors.firstSourceId.id).toBe(CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdAndMaximumNumberOfNames).toBeDefined();
            expect(errors.sourceIdAndMaximumNumberOfNames.id).toBe(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceNamesItems).toBeDefined();
            expect(errors.sourceNamesItems.id).toBe(CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
