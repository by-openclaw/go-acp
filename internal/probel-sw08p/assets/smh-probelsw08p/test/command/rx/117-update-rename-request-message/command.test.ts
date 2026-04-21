import * as _ from 'lodash';
import { UpdateRenameRequestCommand } from '../../../../src/command/rx/117-update-rename-request-message/command';
import { UpdateRenameRequestCommandParams } from '../../../../src/command/rx/117-update-rename-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    UpdateRenameRequestCommandOptions,
    NameType
} from '../../../../src/command/rx/117-update-rename-request-message/options';
import { NameLength } from '../../../../src/command/shared/name-length';

describe('Update Name Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstNameNumber: 0,
                numberOfNamesToFollow: 32,
                nameCharsItems: Fixture.buildCharItems(64, 4)
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.FOUR_CHAR_NAMES,
                nameOfType: NameType.SOURCE_NAME
            };
            // Act
            const command = new UpdateRenameRequestCommand(params, options);

            // Assert
            expect(command).toBeDefined();
            expect(command.params).toBe(params);
            expect(command.options).toBe(options);
            expect(command.identifier).toBe(CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstNameNumber: -1,
                numberOfNamesToFollow: 0,
                nameCharsItems: []
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.SIXTEEN_CHAR_NAMES,
                nameOfType: -1
            };
            // Act
            const fct = () => new UpdateRenameRequestCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstNameNumber.id).toBe(
                    CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceNamesItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstNameNumber: 65536,
                numberOfNamesToFollow: 32,
                nameCharsItems: []
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.SIXTEEN_CHAR_NAMES,
                nameOfType: 25
            };
            // Act
            const fct = () => new UpdateRenameRequestCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstNameNumber.id).toBe(
                    CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceNamesItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Update Name Request Message CMD_117_0X75', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: UpdateRenameRequestCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstNameNumber: 0,
                    numberOfNamesToFollow: 32,
                    nameCharsItems: Fixture.buildCharItems(64, 4)
                };
                const options: UpdateRenameRequestCommandOptions = {
                    lengthOfNames: NameLength.FOUR_CHAR_NAMES,
                    nameOfType: NameType.DESTINATION_ASSOCIATION_NAME
                };
                // Act
                const metaCommand = new UpdateRenameRequestCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '75 02 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 135,
                        checksum: 0x36,
                        buffer:
                            '10 02 75 02 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 87 36 10 03'
                    },
                    {
                        data:
                            '75 02 00 00 00 1f 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 135,
                        checksum: 0xad,
                        buffer:
                            '10 02 75 02 00 00 00 1f 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 87 ad 10 03'
                    }
                ]);
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: UpdateRenameRequestCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstNameNumber: 0,
                    numberOfNamesToFollow: 32,
                    nameCharsItems: Fixture.buildCharItems(64, 4)
                };
                const options: UpdateRenameRequestCommandOptions = {
                    lengthOfNames: NameLength.FOUR_CHAR_NAMES,
                    nameOfType: NameType.SOURCE_NAME
                };
                // Act
                const metaCommand = new UpdateRenameRequestCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '75 00 00 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 136,
                        checksum: 0x37,
                        buffer:
                            '10 02 75 00 00 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 88 37 10 03'
                    },
                    {
                        data:
                            '75 00 00 00 00 00 1f 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 136,
                        checksum: 0xae,
                        buffer:
                            '10 02 75 00 00 00 00 00 1f 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 88 ae 10 03'
                    }
                ]);
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: UpdateRenameRequestCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    firstNameNumber: 16,
                    numberOfNamesToFollow: 16,
                    nameCharsItems: Fixture.buildCharItems(64, 8)
                };
                const options: UpdateRenameRequestCommandOptions = {
                    lengthOfNames: NameLength.EIGHT_CHAR_NAMES,
                    nameOfType: NameType.DESTINATION_ASSOCIATION_NAME
                };
                // Act
                const metaCommand = new UpdateRenameRequestCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '75 02 01 10 00 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35',
                        bytesCount: 135,
                        checksum: 0x8f,
                        buffer:
                            '10 02 75 02 01 10 10 00 10 10 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35 87 8f 10 03'
                    },
                    {
                        data:
                            '75 02 01 10 00 1f 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31',
                        bytesCount: 135,
                        checksum: 0x58,
                        buffer:
                            '10 02 75 02 01 10 10 00 1f 10 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31 87 58 10 03'
                    },
                    {
                        data:
                            '75 02 01 10 00 3f 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37',
                        bytesCount: 135,
                        checksum: 0x22,
                        buffer:
                            '10 02 75 02 01 10 10 00 3f 10 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37 87 22 10 03'
                    },
                    {
                        data:
                            '75 02 01 10 00 70 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33',
                        bytesCount: 135,
                        checksum: 0xdb,
                        buffer:
                            '10 02 75 02 01 10 10 00 70 10 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33 87 db 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the General command', () => {
        it('Should log general - Destination Association Name requested command description', () => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstNameNumber: 0,
                numberOfNamesToFollow: 32,
                nameCharsItems: Fixture.buildCharItems(64, 4)
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.FOUR_CHAR_NAMES,
                nameOfType: NameType.DESTINATION_ASSOCIATION_NAME
            };
            // Act
            const metaCommand = new UpdateRenameRequestCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log general - Source Association Name requested command description', () => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstNameNumber: 0,
                numberOfNamesToFollow: 32,
                nameCharsItems: Fixture.buildCharItems(64, 8)
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.EIGHT_CHAR_NAMES,
                nameOfType: NameType.SOURCE_ASSOCIATION_NAME
            };
            // Act
            const metaCommand = new UpdateRenameRequestCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log general - UMD Label requested command description', () => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstNameNumber: 0,
                numberOfNamesToFollow: 10,
                nameCharsItems: Fixture.buildCharItems(64, 12)
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.TWELVE_CHAR_NAMES,
                nameOfType: NameType.UMD_LABEL
            };
            // Act
            const metaCommand = new UpdateRenameRequestCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
    describe('to Log Description of the general command', () => {
        it('Should log extended - Source Name requested command description', () => {
            // Arrange
            const params: UpdateRenameRequestCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstNameNumber: 0,
                numberOfNamesToFollow: 32,
                nameCharsItems: Fixture.buildCharItems(64, 4)
            };
            const options: UpdateRenameRequestCommandOptions = {
                lengthOfNames: NameLength.FOUR_CHAR_NAMES,
                nameOfType: NameType.SOURCE_NAME
            };
            // Act
            const metaCommand = new UpdateRenameRequestCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('source name')).toBe(true);
        });
    });
});
